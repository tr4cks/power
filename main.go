package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/tr4cks/power/modules"
	"github.com/tr4cks/power/modules/ilo"
	"github.com/tr4cks/power/modules/wakeonlan"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func init() {
	rootCmd.PersistentFlags().StringVar(&configFilePath, "config", path.Join("/etc", fmt.Sprintf("%s.d", appName), "config.yaml"), "YAML configuration file")
	rootCmd.PersistentFlags().StringVarP(&moduleName, "module", "m", "", "module for switching the server on or off")
	rootCmd.MarkPersistentFlagRequired("module")
}

const appName = "power"

var (
	configFilePath string
	moduleName     string
	rootCmd        = &cobra.Command{
		Use:     appName,
		Short:   "All-in-one tool for remote server power control",
		Version: "1.3.1",
		Args:    cobra.NoArgs,
		Run:     run,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
	}
)

type Config struct {
	Username string `validate:"required"`
	Password string `validate:"required"`
	Module   map[string]interface{}
	Discord  *DiscordBotConfig
}

func parseYAMLFile(filePath string) (*Config, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()
	config := Config{}
	decoder := yaml.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		return nil, fmt.Errorf("error decoding YAML file %q: %w", filePath, err)
	}
	return &config, nil
}

func parseConfigFile(filePath string) *Config {
	config, err := parseYAMLFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse YAML file %q: %s\n", filePath, err)
		os.Exit(1)
	}

	validate := validator.New()
	err = validate.Struct(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error during configuration validation: %s\n", err)
		os.Exit(1)
	}

	return config
}

func createModule(config *Config, moduleName string) modules.Module {
	internalModules := map[string]modules.Module{
		"ilo": ilo.New(),
		"wol": wakeonlan.New(),
	}

	module, ok := internalModules[moduleName]
	if !ok {
		moduleNames := make([]string, 0, len(internalModules))
		for moduleName := range internalModules {
			moduleNames = append(moduleNames, moduleName)
		}
		fmt.Fprintf(os.Stderr, "Can't find the %q module among the internal modules (available modules: %s)\n", moduleName, strings.Join(moduleNames, ", "))
		os.Exit(1)
	}

	err := module.Init(config.Module)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error during module initialization: %s\n", err)
		os.Exit(1)
	}

	return module
}

func run(cmd *cobra.Command, args []string) {
	// Logging
	configureLoggers()

	config := parseConfigFile(configFilePath)
	module := createModule(config, moduleName)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := runHttpServer(config, module)

	if config.Discord != nil {
		discordBot, err := NewDiscordBot(config.Discord, module)
		if err != nil {
			mainLogger.Fatal().Err(err).Msg("Unable to create discord bot")
		}
		err = discordBot.Start()
		if err != nil {
			mainLogger.Fatal().Err(err).Msg("Unable to start discord bot")
		}
		defer discordBot.Stop()
	}

	// Listen for the interrupt signal.
	<-ctx.Done()

	// Restore default behavior on the interrupt signal and notify user of shutdown.
	stop()
	mainLogger.Info().Msg("Shutting down gracefully, press Ctrl+C again to force")

	// The context is used to inform the server it has 5 seconds to finish
	// the request it is currently handling
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		mainLogger.Fatal().Err(err).Msg("Server forced to shutdown")
	}

	mainLogger.Info().Msg("Server exiting")
}

//go:embed index.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// loggers
var (
	ginLogger  zerolog.Logger
	mainLogger zerolog.Logger
)

func resolveAddress() string {
	port := os.Getenv("PORT")
	if port != "" {
		mainLogger.
			Debug().
			Msg(fmt.Sprintf("Environment variable PORT=\"%s\"", port))
		return ":" + port
	}
	mainLogger.
		Debug().
		Msg("Environment variable PORT is undefined. Using port :8080 by default")
	return ":8080"
}

func configureLoggers() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	var outputWriter io.Writer = os.Stderr
	if gin.Mode() != "release" {
		outputWriter = zerolog.ConsoleWriter{Out: os.Stderr}
	}
	logger := zerolog.New(outputWriter).With().Timestamp().Logger()
	ginLogger = logger.With().Str("scope", "gin").Logger()
	mainLogger = logger.With().Str("scope", "main").Logger()
}

func loggerWithZerolog(logger *zerolog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)

		if raw != "" {
			path = path + "?" + raw
		}

		errorMessage := c.Errors.ByType(gin.ErrorTypePrivate).String()
		event := logger.Info()
		if errorMessage != "" {
			event = logger.Error()
		}

		event.
			Str("client_ip", c.ClientIP()).
			Str("method", c.Request.Method).
			Str("path", path).
			Int("status", c.Writer.Status()).
			Int("size", c.Writer.Size()).
			Dur("latency", latency).
			Msg(errorMessage)
	}
}

func runHttpServer(config *Config, module modules.Module) *http.Server {
	// Configure Gin
	router := gin.New()
	router.Use(loggerWithZerolog(&ginLogger))
	router.Use(gin.Recovery())
	router.SetTrustedProxies(nil)
	html := template.Must(template.ParseFS(templateFS, "index.html"))
	router.SetHTMLTemplate(html)

	// Serve static folder
	staticSubtreeFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		mainLogger.Fatal().Err(err)
	}
	router.StaticFS("/static", http.FS(staticSubtreeFS))

	withServerState := router.Group("/", ServerStateMiddleware(module, &mainLogger))
	{
		// GET index.html
		withServerState.GET("/", func(c *gin.Context) {
			c.HTML(http.StatusOK, "index.html", gin.H{
				"power": c.GetBool("power"),
				"led":   c.GetBool("led"),
			})
		})

		// POST index.html
		withServerState.POST("/",
			ConditionalMiddleware(func(c *gin.Context) bool { return c.GetBool("power") },
				gin.BasicAuth(gin.Accounts{config.Username: config.Password})),
			func(c *gin.Context) {
				if c.GetBool("power") {
					err := module.PowerOff()
					if err != nil {
						mainLogger.Error().Err(err).Msg("Server shutdown error")
						c.HTML(http.StatusOK, "index.html", gin.H{
							"power": c.GetBool("power"),
							"led":   c.GetBool("led"),
							"error": true,
						})
						return
					}
				} else {
					err := module.PowerOn()
					if err != nil {
						mainLogger.Error().Err(err).Msg("Server power-up error")
						c.HTML(http.StatusOK, "index.html", gin.H{
							"power": c.GetBool("power"),
							"led":   c.GetBool("led"),
							"error": true,
						})
						return
					}
				}

				c.Redirect(http.StatusFound, "/")
			})
	}

	api := router.Group("/api")
	{
		api.POST("/up", func(c *gin.Context) {
			err := module.PowerOn()

			if err != nil {
				mainLogger.Error().Err(err).Msg("Server power-up error")
				c.JSON(http.StatusInternalServerError, gin.H{
					"status": "ko",
					"error":  "a problem occurred during server startup",
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status": "ok",
			})
		})

		api.POST("/down", gin.BasicAuth(gin.Accounts{config.Username: config.Password}), func(c *gin.Context) {
			err := module.PowerOff()

			if err != nil {
				mainLogger.Error().Err(err).Msg("Server shutdown error")
				c.JSON(http.StatusInternalServerError, gin.H{
					"status": "ko",
					"error":  "a problem occurred during server shutdown",
				})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"status": "ok",
			})
		})

		api.GET("/state", ServerStateMiddleware(module, &mainLogger), func(c *gin.Context) {
			c.JSON(200, gin.H{
				"power": c.GetBool("power"),
				"led":   c.GetBool("led"),
			})
		})
	}

	srv := &http.Server{
		Addr:    resolveAddress(),
		Handler: router,
	}

	go func() {
		err := srv.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			mainLogger.Fatal().Err(err).Msg("An error occurred while starting the server")
		}
	}()

	return srv
}

func init() {
	rootCmd.AddCommand(upCmd, downCmd, stateCmd)
}

var (
	upCmd = &cobra.Command{
		Use:   "up",
		Short: "Start the server",
		Run: func(cmd *cobra.Command, args []string) {
			config := parseConfigFile(configFilePath)
			module := createModule(config, moduleName)

			err := module.PowerOn()

			if err != nil {
				fmt.Fprintf(os.Stderr, "Server power-up error: %s\n", err)
				os.Exit(1)
			}
		},
	}
	downCmd = &cobra.Command{
		Use:   "down",
		Short: "Turn off the server",
		Run: func(cmd *cobra.Command, args []string) {
			config := parseConfigFile(configFilePath)
			module := createModule(config, moduleName)

			err := module.PowerOff()

			if err != nil {
				fmt.Fprintf(os.Stderr, "Server shutdown error: %s\n", err)
				os.Exit(1)
			}
		},
	}
	stateCmd = &cobra.Command{
		Use:   "state",
		Short: "Fetch the server state",
		Run: func(cmd *cobra.Command, args []string) {
			config := parseConfigFile(configFilePath)
			module := createModule(config, moduleName)

			powerState, ledState := module.State()

			if powerState.Err != nil {
				fmt.Fprintf(os.Stderr, "Failed to retrieve POWER state: %s\n", powerState.Err)
			}
			if ledState.Err != nil {
				fmt.Fprintf(os.Stderr, "Failed to retrieve LED state: %s\n", ledState.Err)
			}

			state := struct {
				Power bool `json:"power"`
				Led   bool `json:"led"`
			}{
				Power: powerState.Value,
				Led:   ledState.Value,
			}

			jsonString, err := json.Marshal(state)
			if err != nil {
				fmt.Println("Error during JSON conversion:", err)
				os.Exit(1)
			}

			fmt.Println(string(jsonString))
		},
	}
)

func main() {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
