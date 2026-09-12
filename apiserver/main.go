package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap/zapcore"
	"taskwiz.app/core/backend"
	"taskwiz.app/core/config"
	"taskwiz.app/core/frontend"
	auth "taskwiz.app/core/internal/middleware/auth"
	"taskwiz.app/core/internal/migrations"
	"taskwiz.app/core/internal/telemetry"
	database "taskwiz.app/core/internal/utils/database"
	utils "taskwiz.app/core/internal/utils/middleware"
	ws "taskwiz.app/core/internal/ws"

	apis "taskwiz.app/core/internal/apis"
	lRepo "taskwiz.app/core/internal/repos/label"
	nRepo "taskwiz.app/core/internal/repos/notifier"
	sRepo "taskwiz.app/core/internal/repos/session"
	tRepo "taskwiz.app/core/internal/repos/task"
	uRepo "taskwiz.app/core/internal/repos/user"
	lService "taskwiz.app/core/internal/services/labels"
	logging "taskwiz.app/core/internal/services/logging"
	notifier "taskwiz.app/core/internal/services/notifications"
	"taskwiz.app/core/internal/services/scheduler"
	tService "taskwiz.app/core/internal/services/tasks"
	uService "taskwiz.app/core/internal/services/users"
)

func main() {
	cfgFile := flag.String("config", "", "path to config file")
	flag.Parse()

	cfg := config.LoadConfig(*cfgFile)
	if err := config.ValidateCorsConfig(cfg); err != nil {
		log.Fatal(err)
	}
	level, err := zapcore.ParseLevel(cfg.Server.LogLevel)
	if err != nil {
		level = zapcore.WarnLevel
	}

	logging.SetConfig(&logging.Config{
		Encoding:    "console",
		Level:       level,
		Development: level == zapcore.DebugLevel,
	})

	app := fx.New(
		fx.Supply(cfg),
		fx.Supply(logging.DefaultLogger().Desugar()),
		fx.WithLogger(func() fxevent.Logger {
			return &fxevent.NopLogger
		}),

		fx.Provide(auth.NewAuthMiddleware),

		fx.Provide(database.New),
		fx.Provide(tRepo.NewTaskRepository),
		fx.Provide(apis.TasksAPI),
		fx.Provide(uRepo.NewUserRepository),
		fx.Provide(sRepo.NewSessionRepository),
		fx.Provide(nRepo.NewNotificationRepository),
		fx.Provide(apis.UsersAPI),

		// add services
		fx.Provide(notifier.NewNotifier),

		// Rate limiter
		fx.Provide(utils.NewRateLimiter),

		fx.Provide(newServer),
		fx.Provide(ws.NewWSServer),
		fx.Provide(scheduler.NewScheduler),

		fx.Provide(lRepo.NewLabelRepository),
		fx.Provide(lService.NewLabelService),
		fx.Provide(lService.NewLabelsMessageHandler),
		fx.Provide(uService.NewUserService),
		fx.Provide(uService.NewUsersMessageHandler),
		fx.Provide(tService.NewTaskService),
		fx.Provide(tService.NewTasksMessageHandler),
		fx.Provide(apis.LabelsAPI),
		fx.Provide(apis.LogsAPI),

		fx.Provide(frontend.NewHandler),
		fx.Provide(backend.NewHandler),

		fx.Invoke(
			apis.TaskRoutes,
			apis.UserRoutes,
			apis.LabelRoutes,
			apis.LogRoutes,
			ws.Routes,
			tService.TaskMessages,
			lService.LabelMessages,
			uService.UserMessages,
			frontend.Routes,
			backend.Routes,
		),
	)

	if err := app.Err(); err != nil {
		log.Fatal(err)
	}

	app.Run()

}

const (
	defaultReadTimeout       = 2 * time.Second
	defaultWriteTimeout      = 1 * time.Second
	defaultReadHeaderTimeout = 10 * time.Second
	defaultIdleTimeout       = 60 * time.Second
	defaultMaxHeaderBytes    = http.DefaultMaxHeaderBytes
)

func timeoutOrDefault(configured, fallback time.Duration) time.Duration {
	if configured <= 0 {
		return fallback
	}
	return configured
}

func newServer(lc fx.Lifecycle, cfg *config.Config, db *database.DB, bgScheduler *scheduler.Scheduler) *gin.Engine {
	if cfg.Server.LogLevel == "debug" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	trustedProxies := cfg.Server.TrustedProxies
	if trustedProxies == nil {
		trustedProxies = []string{}
	}
	if err := r.SetTrustedProxies(trustedProxies); err != nil {
		log.Fatalf("invalid trusted_proxies configuration: %s", err.Error())
	}
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:           r,
		ReadTimeout:       timeoutOrDefault(cfg.Server.ReadTimeout, defaultReadTimeout),
		WriteTimeout:      timeoutOrDefault(cfg.Server.WriteTimeout, defaultWriteTimeout),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		IdleTimeout:       defaultIdleTimeout,
		MaxHeaderBytes:    defaultMaxHeaderBytes,
	}
	if len(cfg.Server.AllowedOrigins) > 0 {
		corsCfg := cors.DefaultConfig()
		corsCfg.AllowOrigins = cfg.Server.AllowedOrigins
		if cfg.Server.AllowCorsCredentials {
			corsCfg.AllowCredentials = true
		}
		corsCfg.AddAllowHeaders("Authorization")
		corsCfg.AddAllowHeaders("DNT")
		r.Use(cors.New(corsCfg))
	}
	r.Use(utils.SecurityHeaders(cfg))
	r.Use(utils.RequestLogger())
	r.Use(utils.TelemetryMiddleware())

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			logging.FromContext(ctx).Info("Starting server")

			if cfg.Database.Migration {
				runner := migrations.NewRunner(db.RW())
				if err := runner.MigrateUp(ctx, 0); err != nil {
					return fmt.Errorf("failed to run migrations: %s", err.Error())
				}
			}

			bgScheduler.Start(context.Background())

			go func() {
				if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log := logging.FromContext(ctx)
					log.Fatalf("listen: %s\n", err)
				}
			}()

			return nil
		},
		OnStop: func(ctx context.Context) error {
			bgScheduler.Stop()
			telemetry.FlushAppInsights()

			if err := db.Close(); err != nil {
				log := logging.FromContext(ctx)
				log.Errorf("closing database pools: %s", err.Error())
			}

			if err := srv.Shutdown(ctx); err != nil {
				log := logging.FromContext(ctx)
				log.Fatalf("Server Shutdown: %s", err)
			} else {
				log := logging.FromContext(ctx)
				log.Info("Server stopped")
			}
			return nil
		},
	})

	return r
}
