package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"strings"

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
		Version: "1.2.0",
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
	config := parseConfigFile(configFilePath)
	module := createModule(config, moduleName)

	runServer(config, module)
}

//go:embed index.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

func runServer(config *Config, module modules.Module) {
	// Configure Gin
	router := gin.Default()
	router.SetTrustedProxies(nil)
	html := template.Must(template.ParseFS(templateFS, "index.html"))
	router.SetHTMLTemplate(html)

	// Logging
	logErr := log.New(os.Stderr, "[ERR] ", log.LstdFlags|log.Lshortfile)

	// Serve static folder
	staticSubtreeFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal(err)
	}
	router.StaticFS("/static", http.FS(staticSubtreeFS))

	withServerState := router.Group("/", ServerStateMiddleware(module, logErr))
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
						logErr.Printf("Server shutdown error: %s", err)
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
						logErr.Printf("Server power-up error: %s", err)
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
				logErr.Printf("Server power-up error: %s", err)
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
				logErr.Printf("Server shutdown error: %s", err)
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

		api.GET("/state", ServerStateMiddleware(module, logErr), func(c *gin.Context) {
			c.JSON(200, gin.H{
				"power": c.GetBool("power"),
				"led":   c.GetBool("led"),
			})
		})
	}

	router.Run()
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
