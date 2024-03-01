package xcaddycmd

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/caddyserver/xcaddy"
	"github.com/spf13/cobra"
)

func init() {
	buildCommand.Flags().String("with", "", "add plugins by specifying the Go module name and optionally its version, similar to go get. Module name is required, but specific version and/or local replacement are optional.")
	buildCommand.Flags().String("output", "", "changes the output file.")
	buildCommand.Flags().StringSlice("embed", []string{}, "can be used multiple times to embed directories into the built Caddy executable. The directory can be prefixed with a custom alias and a colon : to use it with the root directive and sub-directive.")
}

var buildCommand = &cobra.Command{
	Use:  "build",
	Long: "",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var output string
		var plugins []xcaddy.Dependency
		var replacements []xcaddy.Replace
		var embedDir []string
		var argCaddyVersion string
		if len(args) > 0 {
			argCaddyVersion = args[0]
		}
		withArg, err := cmd.Flags().GetString("with")

		if err != nil {
			return fmt.Errorf("unable to parse --with arguments: %s", err.Error())
		}

		if withArg != "" {
			mod, ver, repl, err := splitWith(withArg)
			if err != nil {
				return err
			}
			mod = strings.TrimSuffix(mod, "/") // easy to accidentally leave a trailing slash if pasting from a URL, but is invalid for Go modules
			plugins = append(plugins, xcaddy.Dependency{
				PackagePath: mod,
				Version:     ver,
			})
			if repl != "" {
				// adjust relative replacements in current working directory since our temporary module is in a different directory
				if strings.HasPrefix(repl, ".") {
					repl, err = filepath.Abs(repl)
					if err != nil {
						log.Fatalf("[FATAL] %v", err)
					}
					log.Printf("[INFO] Resolved relative replacement %s to %s", withArg, repl)
				}
				replacements = append(replacements, xcaddy.NewReplace(mod, repl))
			}
		}

		output, err = cmd.Flags().GetString("output")
		if err != nil {
			return fmt.Errorf("unable to parse --output arguments: %s", err.Error())
		}

		embedDir, err = cmd.Flags().GetStringSlice("embed")
		if err != nil {
			return fmt.Errorf("unable to parse --embed arguments: %s", err.Error())
		}
		// prefer caddy version from command line argument over env var
		if argCaddyVersion != "" {
			caddyVersion = argCaddyVersion
		}

		// ensure an output file is always specified
		if output == "" {
			output = getCaddyOutputFile()
		}

		// perform the build
		builder := xcaddy.Builder{
			Compile: xcaddy.Compile{
				Cgo: os.Getenv("CGO_ENABLED") == "1",
			},
			CaddyVersion: caddyVersion,
			Plugins:      plugins,
			Replacements: replacements,
			RaceDetector: raceDetector,
			SkipBuild:    skipBuild,
			SkipCleanup:  skipCleanup,
			Debug:        buildDebugOutput,
			BuildFlags:   buildFlags,
			ModFlags:     modFlags,
		}
		for _, md := range embedDir {
			if before, after, found := strings.Cut(md, ":"); found {
				builder.EmbedDirs = append(builder.EmbedDirs, struct {
					Dir  string `json:"dir,omitempty"`
					Name string `json:"name,omitempty"`
				}{
					after, before,
				})
			} else {
				builder.EmbedDirs = append(builder.EmbedDirs, struct {
					Dir  string `json:"dir,omitempty"`
					Name string `json:"name,omitempty"`
				}{
					before, "",
				})
			}
		}
		err = builder.Build(cmd.Root().Context(), output)
		if err != nil {
			log.Fatalf("[FATAL] %v", err)
		}

		// if requested, run setcap to allow binding to low ports
		err = setcapIfRequested(output)
		if err != nil {
			return err
		}

		// prove the build is working by printing the version
		if runtime.GOOS == os.Getenv("GOOS") && runtime.GOARCH == os.Getenv("GOARCH") {
			if !filepath.IsAbs(output) {
				output = "." + string(filepath.Separator) + output
			}
			fmt.Println()
			fmt.Printf("%s version\n", output)
			cmd := exec.Command(output, "version")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err = cmd.Run()
			if err != nil {
				log.Fatalf("[FATAL] %v", err)
			}
		}

		return nil
	},
}
