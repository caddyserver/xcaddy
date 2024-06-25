package xcaddycmd

import (
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/caddyserver/xcaddy"
	"github.com/caddyserver/xcaddy/internal/utils"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:          "xcaddy",
	Long:         "",
	SilenceUsage: true,
	Version:      xcaddyVersion(),
	RunE: func(cmd *cobra.Command, args []string) error {
		binOutput := getCaddyOutputFile()

		// get current/main module name and the root directory of the main module
		//
		// make sure the module being developed is replaced
		// so that the local copy is used
		//
		// replace directives only apply to the top-level/main go.mod,
		// and since this tool is a carry-through for the user's actual
		// go.mod, we need to transfer their replace directives through
		// to the one we're making
		execCmd := exec.Command(utils.GetGo(), "list", "-mod=readonly", "-m", "-json", "all")
		execCmd.Stderr = os.Stderr
		out, err := execCmd.Output()
		if err != nil {
			return fmt.Errorf("exec %v: %v: %s", cmd.Args, err, string(out))
		}
		currentModule, moduleDir, replacements, err := parseGoListJson(out)
		if err != nil {
			return fmt.Errorf("json parse error: %v", err)
		}

		// reconcile remaining path segments; for example if a module foo/a
		// is rooted at directory path /home/foo/a, but the current directory
		// is /home/foo/a/b, then the package to import should be foo/a/b
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("unable to determine current directory: %v", err)
		}
		importPath := normalizeImportPath(currentModule, cwd, moduleDir)

		// build caddy with this module plugged in
		builder := xcaddy.Builder{
			Compile: xcaddy.Compile{
				Cgo: os.Getenv("CGO_ENABLED") == "1",
			},
			CaddyVersion: caddyVersion,
			Plugins: []xcaddy.Dependency{
				{PackagePath: importPath},
			},
			Replacements: replacements,
			RaceDetector: raceDetector,
			SkipBuild:    skipBuild,
			SkipCleanup:  skipCleanup,
			Debug:        buildDebugOutput,
		}
		err = builder.Build(cmd.Context(), binOutput)
		if err != nil {
			return err
		}

		// if requested, run setcap to allow binding to low ports
		err = setcapIfRequested(binOutput)
		if err != nil {
			return err
		}

		log.Printf("[INFO] Running %v\n\n", append([]string{binOutput}, args...))

		execCmd = exec.Command(binOutput, args...)
		execCmd.Stdin = os.Stdin
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr
		err = execCmd.Start()
		if err != nil {
			return err
		}
		defer func() {
			if skipCleanup {
				log.Printf("[INFO] Skipping cleanup as requested; leaving artifact: %s", binOutput)
				return
			}
			err = os.Remove(binOutput)
			if err != nil && !os.IsNotExist(err) {
				log.Printf("[ERROR] Deleting temporary binary %s: %v", binOutput, err)
			}
		}()

		return execCmd.Wait()
	},
}

const fullDocsFooter = `Full documentation is available at:
https://github.com/caddyserver/xcaddy`

func init() {
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.SetHelpTemplate(rootCmd.HelpTemplate() + "\n" + fullDocsFooter + "\n")
	rootCmd.AddCommand(buildCommand)
	rootCmd.AddCommand(versionCommand)
}
