package main

import (
	"log"
	"os"

	"github.com/alecthomas/kong"
	"github.com/cskr/pubsub"
	"gopkg.in/natefinch/lumberjack.v2"

	"unbalance/daemon/cmd"
	"unbalance/daemon/domain"
	"unbalance/daemon/lib"
)

var Version string

var cli struct {
	Port    string `name:"port" default:"7090" help:"port to listen on"`
	LogsDir string `name:"logs-dir" env:"UNBALANCED_LOGS_DIR" default:"/var/log" help:"directory to store logs"`
	DataDir string `name:"data-dir" env:"UNBALANCED_DATA_DIR" default:"" help:"directory for mutable state (env, history, sessions); defaults to the official plugin path"`

	// Config vars. Kong may initialize these from process environment or
	// defaults (DRY_RUN defaults to true). After path resolution, data-dir
	// unbalanced.env overlays present keys and is the runtime source of truth.
	DryRun         bool     `env:"DRY_RUN" default:"true" help:"perform a dry-run rather than actual work"`
	NotifyPlan     int      `env:"NOTIFY_PLAN" default:"0" help:"notify via email after plan operation has completed (unraid notifications must be set up first): 0 - No notifications; 1 - Simple notifications; 2 - Detailed notifications"`
	NotifyTransfer int      `env:"NOTIFY_TRANSFER" default:"0" help:"notify via email after transfer operation has completed (unraid notifications must be set up first): 0 - No notifications; 1 - Simple notifications; 2 - Detailed notifications"`
	ReservedAmount uint64   `env:"RESERVED_AMOUNT" default:"1" help:"Minimun Amount of space to reserve"`
	ReservedUnit   string   `env:"RESERVED_UNIT" default:"Gb" help:"Reserved Amount unit: Gb or %"`
	RsyncArgs      []string `env:"RSYNC_ARGS" default:"-X" help:"custom rsync arguments"`
	Verbosity      int      `env:"VERBOSITY" default:"0" help:"include rsync output in log files: 0 (default) - include; 1 - do not include"`
	RefreshRate    int      `env:"REFRESH_RATE" default:"1000" help:"how often to refresh the ui while running a command (in milliseconds)"`
	LogLines       int      `env:"LOG_LINES" default:"100" help:"number of log lines shown in the web ui logs page"`
	SpeedWindow    string   `env:"SPEED_WINDOW" default:"90s" help:"time window used to calculate recent transfer speed"`
	TvLibraryPath  string   `env:"TV_LIBRARY_PATH" default:"data/media/tv" help:"TV library path relative to /mnt/user used by Auto Gather"`
	AuthEnabled    bool     `env:"AUTH_ENABLED" default:"false" help:"require login before using the web ui"`
	AuthUsername   string   `env:"AUTH_USERNAME" default:"admin" help:"admin username used to log into the web ui"`
	AuthPassword   string   `env:"AUTH_PASSWORD_HASH" default:"" help:"stored admin password hash (Argon2id for new passwords; bcrypt remains supported for migration)"`

	Boot cmd.Boot `cmd:"" default:"1" help:"start processing"`
}

func main() {
	kctx := kong.Parse(&cli)

	paths, err := domain.ResolveRuntimePaths(
		cli.DataDir,
		cli.LogsDir,
		domain.ResolveRuntimeOptions{
			EmptyDataDirExplicit: domain.EmptyDataDirExplicit(os.Args, os.Environ()),
			EmptyLogsDirExplicit: domain.EmptyLogsDirExplicit(os.Args, os.Environ()),
			CustomLogsDir:        domain.LogsDirProvided(os.Args, os.Environ()),
		},
	)
	if err != nil {
		log.Fatalf("runtime paths: %s", err)
	}

	if err := domain.EnsureRuntimeDirs(paths); err != nil {
		log.Fatalf("runtime directories: %s", err)
	}

	log.SetOutput(&lumberjack.Logger{
		Filename:   paths.LogFile,
		MaxSize:    10, // megabytes
		MaxBackups: 10,
		MaxAge:     28, // days
	})

	// Read AUTH_PASSWORD_HASH from the resolved env file to sidestep bash's
	// mangling of $-characters when the start script sources the env file.
	if _, statErr := os.Stat(paths.EnvFile); statErr == nil {
		if hash, err := lib.LoadAuthHash(paths.EnvFile); err != nil {
			log.Printf("warning: unable to read auth hash from %s: %s", paths.EnvFile, err)
		} else {
			cli.AuthPassword = hash
		}
	} else if !os.IsNotExist(statErr) {
		log.Printf("warning: unable to stat env file %s: %s", paths.EnvFile, statErr)
	}

	config := domain.Config{
		Version:        Version,
		DryRun:         cli.DryRun,
		NotifyPlan:     cli.NotifyPlan,
		NotifyTransfer: cli.NotifyTransfer,
		ReservedAmount: cli.ReservedAmount,
		ReservedUnit:   cli.ReservedUnit,
		RsyncArgs:      cli.RsyncArgs,
		Verbosity:      cli.Verbosity,
		RefreshRate:    cli.RefreshRate,
		LogLines:       cli.LogLines,
		SpeedWindow:    cli.SpeedWindow,
		TvLibraryPath:  cli.TvLibraryPath,
		AuthEnabled:    cli.AuthEnabled,
		AuthUsername:   cli.AuthUsername,
		AuthPassword:   cli.AuthPassword,
	}
	if err := lib.ApplyPersistedEnv(paths.EnvFile, &config); err != nil {
		log.Printf("warning: unable to load env file %s: %s", paths.EnvFile, err)
	} else {
		cli.DryRun = config.DryRun
		cli.TvLibraryPath = config.TvLibraryPath
		cli.AuthPassword = config.AuthPassword
	}

	dataDirLog := cli.DataDir
	if dataDirLog == "" {
		dataDirLog = "(default)"
	}
	log.Printf("cli: port=%s logsDir=%s dataDir=%s dryRun=%v authEnabled=%v authUsername=%s tvLibraryPath=%s",
		cli.Port, cli.LogsDir, dataDirLog, cli.DryRun, cli.AuthEnabled, cli.AuthUsername, cli.TvLibraryPath)
	log.Printf("runtime: port=%s dataDir=%s envFile=%s historyFile=%s sessionsFile=%s logFile=%s",
		cli.Port,
		paths.DataDir,
		paths.EnvFile,
		paths.HistoryFile,
		paths.SessionsFile,
		paths.LogFile,
	)

	err = kctx.Run(&domain.Context{
		Port:    cli.Port,
		LogsDir: paths.LogsDir,
		DataDir: paths.DataDir,
		Paths:   paths,
		Config:  config,
		Hub:     pubsub.New(23),
	})
	kctx.FatalIfErrorf(err)
}
