package main // import "github.com/Jguer/yay"

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime/debug"

	"github.com/leonelquinteros/gotext"

	"github.com/Jguer/yay/v12/pkg/db"
	"github.com/Jguer/yay/v12/pkg/db/ialpm"
	"github.com/Jguer/yay/v12/pkg/query"
	"github.com/Jguer/yay/v12/pkg/settings"
	"github.com/Jguer/yay/v12/pkg/settings/parser"
	"github.com/Jguer/yay/v12/pkg/text"
)

var (
	yayVersion = "12.0.0"            // To be set by compiler.
	localePath = "/usr/share/locale" // To be set by compiler.
)

func initGotext() {
	if envLocalePath := os.Getenv("LOCALE_PATH"); envLocalePath != "" {
		localePath = envLocalePath
	}

	if lc := os.Getenv("LANGUAGE"); lc != "" {
		gotext.Configure(localePath, lc, "yay")
	} else if lc := os.Getenv("LC_ALL"); lc != "" {
		gotext.Configure(localePath, lc, "yay")
	} else if lc := os.Getenv("LC_MESSAGES"); lc != "" {
		gotext.Configure(localePath, lc, "yay")
	} else {
		gotext.Configure(localePath, os.Getenv("LANG"), "yay")
	}
}

func main() {
	var (
		err error
		ctx = context.Background()
		ret = 0
	)

	defer func() {
		if rec := recover(); rec != nil {
			text.Errorln(rec)
			debug.PrintStack()
		}

		os.Exit(ret)
	}()

	initGotext()

	if os.Geteuid() == 0 {
		text.Warnln(gotext.Get("Avoid running yay as root/sudo."))
	}

	configPath := settings.GetConfigPath()
	// Parse config
	cfg, err := settings.NewConfig(configPath, yayVersion)
	if err != nil {
		if str := err.Error(); str != "" {
			text.Errorln(str)
		}

		ret = 1

		return
	}

	if cfg.Debug {
		text.GlobalLogger.Debug = true
	}

	if errS := cfg.RunMigrations(
		settings.DefaultMigrations(), configPath, yayVersion); errS != nil {
		text.Errorln(errS)
	}

	cmdArgs := parser.MakeArguments()

	// Parse command line
	if err = cfg.ParseCommandLine(cmdArgs); err != nil {
		if str := err.Error(); str != "" {
			text.Errorln(str)
		}

		ret = 1

		return
	}

	if cfg.SaveConfig {
		if errS := cfg.Save(configPath, yayVersion); errS != nil {
			text.Errorln(errS)
		}
	}

	// Build runtime
	runtime, err := settings.BuildRuntime(cfg, cmdArgs, yayVersion)
	if err != nil {
		if str := err.Error(); str != "" {
			text.Errorln(str)
		}

		ret = 1

		return
	}

	cfg.Runtime = runtime

	if cfg.SeparateSources {
		cfg.Runtime.QueryBuilder = query.NewSourceQueryBuilder(
			cfg.Runtime.AURClient, cfg.Runtime.AURCache,
			cfg.Runtime.Logger.Child("querybuilder"), cfg.SortBy,
			cfg.Mode, cfg.SearchBy, cfg.BottomUp,
			cfg.SingleLineResults, cfg.NewInstallEngine)
	} else {
		cfg.Runtime.QueryBuilder = query.NewMixedSourceQueryBuilder(
			cfg.Runtime.AURClient, cfg.Runtime.AURCache,
			cfg.Runtime.Logger.Child("mixed.querybuilder"), cfg.SortBy,
			cfg.Mode, cfg.SearchBy,
			cfg.BottomUp, cfg.SingleLineResults, cfg.NewInstallEngine)
	}

	var useColor bool

	cfg.Runtime.PacmanConf, useColor, err = settings.RetrievePacmanConfig(cmdArgs, cfg.PacmanConf)
	if err != nil {
		if str := err.Error(); str != "" {
			text.Errorln(str)
		}

		ret = 1

		return
	}

	cfg.Runtime.CmdBuilder.SetPacmanDBPath(cfg.Runtime.PacmanConf.DBPath)

	text.UseColor = useColor

	dbExecutor, err := ialpm.NewExecutor(cfg.Runtime.PacmanConf)
	if err != nil {
		if str := err.Error(); str != "" {
			text.Errorln(str)
		}

		ret = 1

		return
	}

	defer func() {
		if rec := recover(); rec != nil {
			text.Errorln(rec)
			debug.PrintStack()
		}

		dbExecutor.Cleanup()
	}()

	if err = handleCmd(ctx, cfg, cmdArgs, db.Executor(dbExecutor)); err != nil {
		if str := err.Error(); str != "" {
			text.Errorln(str)
		}

		exitError := &exec.ExitError{}
		if errors.As(err, &exitError) {
			// mirror pacman exit code when applicable
			ret = exitError.ExitCode()
			return
		}

		// fallback
		ret = 1
	}
}
