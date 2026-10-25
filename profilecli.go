package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/ensamuel7/dex/profview"
)

// profileBuild is set by `dex profile`, and asks configureGen for the profiler
// the way DEX_PROFILE=1 does.
var profileBuild bool

// runProfile is `dex profile <file.dx>`: build with the memory profiler, run
// the program, and turn what it wrote at exit into build/<name>.profile.html.
// Given a .json written earlier — by a run with DEX_PROFILE=1, or a server's
// SIGUSR1 snapshot — it renders that instead of building anything.
func runProfile(target string) {
	if strings.HasSuffix(target, ".json") {
		raw, err := os.ReadFile(target)
		exitOn(err)
		name := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(target), ".json"), ".profile")
		htmlPath := strings.TrimSuffix(target, ".json") + ".html"
		exitOn(writeProfilePage(raw, name, htmlPath))
		return
	}

	profileBuild = true
	binaryPath, err := build(target)
	if err != nil {
		fmt.Fprintln(os.Stderr, formatError(err.Error()))
		os.Exit(1)
	}
	defer os.Remove(binaryPath)

	jsonPath := binaryPath + ".profile.json"
	htmlPath := binaryPath + ".profile.html"
	absJSON, err := filepath.Abs(jsonPath)
	exitOn(err)
	// A profile left by an earlier run must not be mistaken for this one's.
	os.Remove(jsonPath)

	cmd := exec.Command(binaryPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "DEX_PROFILE_OUT="+absJSON)

	// Ctrl-C reaches the whole process group, so the program gets it directly
	// and writes its profile on the way down. This process has to outlive it
	// to render the page, so it only passes a SIGTERM on.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	exitCode := 0
	if err := cmd.Start(); err != nil {
		exitOn(err)
	}
	go func() {
		for s := range sigs {
			if s == syscall.SIGTERM && cmd.Process != nil {
				cmd.Process.Signal(s)
			}
		}
	}()
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			fmt.Fprintln(os.Stderr, err)
			exitCode = 1
		}
	}
	signal.Stop(sigs)
	close(sigs)

	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "profile: the program wrote no profile — it ended without running its exit handlers (a crash, or a signal it does not let the profiler see)")
		if exitCode == 0 {
			exitCode = 1
		}
		os.Exit(exitCode)
	}
	name := strings.TrimSuffix(filepath.Base(target), filepath.Ext(target))
	if err := writeProfilePage(raw, name, htmlPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

// writeProfilePage renders a profile to htmlPath and opens it unless
// --no-open was given.
func writeProfilePage(raw []byte, name, htmlPath string) error {
	cwd, _ := os.Getwd()
	page, err := profview.Render(raw, name, cwd)
	if err != nil {
		return err
	}
	if err := os.WriteFile(htmlPath, page, 0644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "profile: report at %s\n", htmlPath)
	if !noOpen {
		openInBrowser(htmlPath)
	}
	return nil
}

// openInBrowser hands a file to the desktop's opener. Failing to open is not
// an error: the path has been printed, and a headless machine has no browser.
func openInBrowser(path string) {
	var opener string
	switch runtime.GOOS {
	case "darwin":
		opener = "open"
	case "linux", "freebsd", "openbsd", "netbsd":
		opener = "xdg-open"
	default:
		return
	}
	if _, err := exec.LookPath(opener); err != nil {
		return
	}
	cmd := exec.Command(opener, path)
	cmd.Start()
	go cmd.Wait()
}
