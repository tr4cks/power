package main

import (
	"embed"
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
	rootCmd.Flags().StringVar(&configFilePath, "config", path.Join("/opt", appName, "config.yaml"), "YAML configuration file")
	rootCmd.Flags().StringVarP(&moduleName, "module", "m", "", "module for switching the server on or off")
	rootCmd.MarkFlagRequired("module")
}

const appName = "power"

var (
	configFilePath string
	moduleName     string
	rootCmd        = &cobra.Command{
		Use:     appName,
		Short:   "All-in-one tool for remote server power control",
		Version: "1.0.0",
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

func run(cmd *cobra.Command, args []string) {
	config, err := parseYAMLFile(configFilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse YAML file %q: %s\n", configFilePath, err)
		os.Exit(1)
	}

	validate := validator.New()
	err = validate.Struct(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error during configuration validation: %s\n", err)
		os.Exit(1)
	}

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

	err = module.Init(config.Module)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error during module initialization: %s\n", err)
		os.Exit(1)
		return
	}

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
						logErr.Printf("Server power-up error: %s", err)
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
						logErr.Printf("Server shutdown error: %s", err)
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

	router.Run()
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
