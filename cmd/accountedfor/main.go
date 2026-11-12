// Command accountedfor runs the Accountedfor "I'm safe" board and its
// administration tasks.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
	_ "time/tzdata" // Time zones work in minimal containers too.

	"github.com/lobojoel2003/accountedfor/internal/config"
	"github.com/lobojoel2003/accountedfor/internal/exchange"
	"github.com/lobojoel2003/accountedfor/internal/store"
	"github.com/lobojoel2003/accountedfor/internal/version"
	"github.com/lobojoel2003/accountedfor/internal/web"
)

const usage = `Accountedfor — an "I'm safe" board for disasters.

Usage:
  accountedfor [command] [arguments]

Commands:
  serve                      Run the web server (default)
  migrate                    Apply database migrations and exit
  user add NAME [--admin] [--display "Full Name"]
                             Create a responder account and print its password
  user list                  List accounts
  user passwd NAME           Set a new random password for an account
  user disable|enable NAME   Disable or re-enable an account
  token create NAME [--write]
                             Create a PFIF API token (read-only unless --write)
  token list                 List API tokens
  token revoke ID            Revoke an API token
  import FILE [--visibility public|limited|private]
                             Import a PFIF 1.1-1.4 XML file ("-" for stdin)
  export [--since RFC3339] [--public]
                             Write records as PFIF 1.4 XML to stdout
  purge                      Delete expired records now
  healthcheck [URL]          Exit 0 if the server answers /healthz
  version                    Print version and build information

Configuration is read from environment variables; see .env.example.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "version", "--version", "-v":
		fmt.Println("accountedfor", version.String())
		return nil
	case "help", "--help", "-h":
		fmt.Print(usage)
		return nil
	case "healthcheck":
		return healthcheck(args)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.RequireDatabase(); err != nil {
		return err
	}
	log := newLogger(cfg)
	store.Domain = cfg.PFIFDomain

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return err
	}

	switch cmd {
	case "serve":
		return serve(ctx, cfg, st, log)
	case "migrate":
		v, err := st.SchemaVersion(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("database schema is at version %d\n", v)
		return nil
	case "user":
		return userCmd(ctx, st, args)
	case "token":
		return tokenCmd(ctx, st, args)
	case "import":
		return importCmd(ctx, cfg, st, args)
	case "export":
		return exportCmd(ctx, st, args)
	case "purge":
		n, err := st.PurgeExpired(ctx, time.Now())
		if err != nil {
			return err
		}
		fmt.Printf("deleted %d expired records\n", n)
		return nil
	}
	fmt.Fprint(os.Stderr, usage)
	return fmt.Errorf("unknown command %q", cmd)
}

func newLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler = slog.NewTextHandler(os.Stderr, opts)
	if cfg.LogFormat == "json" {
		h = slog.NewJSONHandler(os.Stderr, opts)
	}
	return slog.New(h)
}

func serve(ctx context.Context, cfg *config.Config, st *store.Store, log *slog.Logger) error {
	if err := bootstrapAdmin(ctx, cfg, st, log); err != nil {
		return err
	}
	srv, err := web.New(cfg, st, log)
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}

	go purgeLoop(ctx, st, log)

	errc := make(chan error, 1)
	go func() {
		log.Info("accountedfor listening", "addr", cfg.ListenAddr, "base_url", cfg.BaseURL.String(),
			"version", version.String())
		errc <- httpServer.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

func bootstrapAdmin(ctx context.Context, cfg *config.Config, st *store.Store, log *slog.Logger) error {
	n, err := st.CountUsers(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if cfg.BootstrapAdminPassword == "" {
		log.Warn("no responder accounts exist; create one with: accountedfor user add NAME --admin")
		return nil
	}
	if _, err := st.CreateUser(ctx, cfg.BootstrapAdminUser, "", cfg.BootstrapAdminPassword, store.RoleAdmin); err != nil {
		return fmt.Errorf("create bootstrap admin: %w", err)
	}
	log.Info("created administrator account from BOOTSTRAP_ADMIN_PASSWORD", "username", cfg.BootstrapAdminUser)
	return nil
}

// purgeLoop deletes expired records every hour.
func purgeLoop(ctx context.Context, st *store.Store, log *slog.Logger) {
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	for {
		n, err := st.PurgeExpired(ctx, time.Now())
		if err != nil && ctx.Err() == nil {
			log.Error("purge expired records", "err", err)
		} else if n > 0 {
			log.Info("purged expired records", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func healthcheck(args []string) error {
	target := "http://127.0.0.1:8080/healthz"
	if port := os.Getenv("PORT"); port != "" {
		target = "http://127.0.0.1:" + port + "/healthz"
	}
	if addr := os.Getenv("LISTEN_ADDR"); strings.HasPrefix(addr, ":") {
		target = "http://127.0.0.1" + addr + "/healthz"
	}
	if len(args) > 0 {
		target = args[0]
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", target, resp.Status)
	}
	return nil
}

func userCmd(ctx context.Context, st *store.Store, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: accountedfor user add|list|passwd|disable|enable")
	}
	sub, args := args[0], args[1:]
	switch sub {
	case "list":
		users, err := st.ListUsers(ctx)
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "USERNAME\tROLE\tSTATUS\tLAST LOGIN")
		for _, u := range users {
			status := "active"
			if u.Disabled {
				status = "disabled"
			}
			last := "never"
			if u.LastLoginAt != nil {
				last = u.LastLoginAt.Format(time.RFC3339)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", u.Username, u.Role, status, last)
		}
		return tw.Flush()
	case "add":
		fs := flag.NewFlagSet("user add", flag.ContinueOnError)
		admin := fs.Bool("admin", false, "grant administrator rights")
		display := fs.String("display", "", "display name")
		password := fs.String("password", "", "password (default: generate one)")
		name, rest := splitName(args)
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if name == "" {
			name = fs.Arg(0)
		}
		if name == "" {
			return errors.New("usage: accountedfor user add NAME [--admin] [--display NAME]")
		}
		role := store.RoleResponder
		if *admin {
			role = store.RoleAdmin
		}
		pw := *password
		if pw == "" {
			pw = web.NewPassword()
		}
		u, err := st.CreateUser(ctx, name, *display, pw, role)
		if err != nil {
			return err
		}
		_ = st.Audit(ctx, "cli", "user_create", u.Username+" ("+role+")")
		fmt.Printf("created %s %q\npassword: %s\n", role, u.Username, pw)
		return nil
	case "passwd":
		if len(args) != 1 {
			return errors.New("usage: accountedfor user passwd NAME")
		}
		pw := web.NewPassword()
		if err := st.SetPassword(ctx, args[0], pw); err != nil {
			return err
		}
		_ = st.Audit(ctx, "cli", "user_reset_password", args[0])
		fmt.Printf("new password for %q: %s\n", args[0], pw)
		return nil
	case "disable", "enable":
		if len(args) != 1 {
			return fmt.Errorf("usage: accountedfor user %s NAME", sub)
		}
		if err := st.SetUserDisabled(ctx, args[0], sub == "disable"); err != nil {
			return err
		}
		_ = st.Audit(ctx, "cli", "user_"+sub, args[0])
		fmt.Printf("%sd %q\n", sub, args[0])
		return nil
	}
	return fmt.Errorf("unknown user command %q", sub)
}

// splitName takes a leading positional argument so flags may follow it.
// A lone "-" (standard input) counts as positional.
func splitName(args []string) (string, []string) {
	if len(args) > 0 && (args[0] == "-" || !strings.HasPrefix(args[0], "-")) {
		return args[0], args[1:]
	}
	return "", args
}

func tokenCmd(ctx context.Context, st *store.Store, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: accountedfor token create|list|revoke")
	}
	sub, args := args[0], args[1:]
	switch sub {
	case "create":
		fs := flag.NewFlagSet("token create", flag.ContinueOnError)
		write := fs.Bool("write", false, "allow importing records")
		name, rest := splitName(args)
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if name == "" {
			return errors.New("usage: accountedfor token create NAME [--write]")
		}
		scope := "read"
		if *write {
			scope = "readwrite"
		}
		t, secret, err := st.CreateAPIToken(ctx, name, scope)
		if err != nil {
			return err
		}
		_ = st.Audit(ctx, "cli", "token_create", fmt.Sprintf("%d %s (%s)", t.ID, t.Name, t.Scope))
		fmt.Printf("token %d %q (%s)\n%s\n", t.ID, t.Name, t.Scope, secret)
		return nil
	case "list":
		tokens, err := st.ListAPITokens(ctx)
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tNAME\tSCOPE\tSTATUS")
		for _, t := range tokens {
			status := "active"
			if t.RevokedAt != nil {
				status = "revoked"
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", t.ID, t.Name, t.Scope, status)
		}
		return tw.Flush()
	case "revoke":
		if len(args) != 1 {
			return errors.New("usage: accountedfor token revoke ID")
		}
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid token id %q", args[0])
		}
		if err := st.RevokeAPIToken(ctx, id); err != nil {
			return err
		}
		_ = st.Audit(ctx, "cli", "token_revoke", args[0])
		fmt.Printf("revoked token %d\n", id)
		return nil
	}
	return fmt.Errorf("unknown token command %q", sub)
}

func importCmd(ctx context.Context, cfg *config.Config, st *store.Store, args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	vis := fs.String("visibility", "limited", "visibility of new records: public, limited or private")
	file, rest := splitName(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if file == "" {
		return errors.New("usage: accountedfor import FILE [--visibility limited]")
	}
	var r io.Reader = os.Stdin
	if file != "-" {
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		r = f
	}
	v := store.Visibility(*vis)
	if !v.Valid() {
		return fmt.Errorf("visibility must be public, limited or private")
	}
	res, err := exchange.Import(ctx, st, r, exchange.ImportOptions{Visibility: v, Retention: cfg.Retention()})
	if err != nil {
		return err
	}
	_ = st.Audit(ctx, "cli", "import_pfif", file+": "+res.Summary())
	fmt.Println(res.Summary())
	for _, p := range res.Problems {
		fmt.Println("  -", p)
	}
	return nil
}

func exportCmd(ctx context.Context, st *store.Store, args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	since := fs.String("since", "", "only records changed since this RFC 3339 time")
	public := fs.Bool("public", false, "export only public information")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opt := exchange.ExportOptions{Level: exchange.Full, Tombstones: true}
	if *public {
		opt.Level = exchange.Public
	}
	if *since != "" {
		t, err := time.Parse(time.RFC3339, *since)
		if err != nil {
			return fmt.Errorf("--since: %w", err)
		}
		opt.Since = t
	}
	n, err := exchange.Export(ctx, st, os.Stdout, opt)
	if err != nil {
		return err
	}
	_ = st.Audit(ctx, "cli", "export_pfif", fmt.Sprintf("%d records", n))
	fmt.Fprintf(os.Stderr, "exported %d records\n", n)
	return nil
}
