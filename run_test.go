package playwright

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/mitchellh/go-ps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunOptionsRedirectStderr(t *testing.T) {
	r, w := io.Pipe()
	var output string
	wg := &sync.WaitGroup{}
	readIOAsyncTilEOF(t, r, wg, &output)

	driverPath := t.TempDir()
	options := &RunOptions{
		Stderr:          w,
		DriverDirectory: driverPath,
		Browsers:        []string{},
		Verbose:         true,
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer ts.Close()

	t.Setenv("PLAYWRIGHT_DOWNLOAD_HOST", ts.URL)
	driver, err := NewDriver(options)
	require.NoError(t, err)
	err = driver.Install()
	require.Error(t, err)
	require.NoError(t, w.Close())
	wg.Wait()

	assert.Contains(t, output, "Downloading driver")
	require.Contains(t, output, fmt.Sprintf("path=%s", driverPath))
}

func TestRunOptions_OnlyInstallShell(t *testing.T) {
	if getBrowserName() != "chromium" {
		t.Skip("chromium only")
		return
	}

	r, w := io.Pipe()
	var output string
	wg := &sync.WaitGroup{}
	readIOAsyncTilEOF(t, r, wg, &output)

	driverPath := t.TempDir()
	driver, err := NewDriver(&RunOptions{
		Stdout:           w,
		DriverDirectory:  driverPath,
		Browsers:         []string{getBrowserName()},
		Verbose:          true,
		OnlyInstallShell: true,
		DryRun:           true,
	})
	require.NoError(t, err)
	browserPath := t.TempDir()

	t.Setenv("PLAYWRIGHT_BROWSERS_PATH", browserPath)

	err = driver.Install()
	require.NoError(t, err)
	require.NoError(t, w.Close())
	wg.Wait()

	assert.Contains(t, output, "browser: chromium-headless-shell version")
	assert.NotContains(t, output, "browser: chromium version")
}

func TestDriverInstall(t *testing.T) {
	driverPath := t.TempDir()
	driver, err := NewDriver(&RunOptions{
		DriverDirectory: driverPath,
		Browsers:        []string{getBrowserName()},
		Verbose:         true,
	})
	if err != nil {
		t.Fatalf("could not start driver: %v", err)
	}
	browserPath := t.TempDir()
	err = os.Setenv("PLAYWRIGHT_BROWSERS_PATH", browserPath)
	if err != nil {
		t.Fatalf("could not set PLAYWRIGHT_BROWSERS_PATH: %v", err)
	}
	defer os.Unsetenv("PLAYWRIGHT_BROWSERS_PATH")
	err = driver.Install()
	if err != nil {
		t.Fatalf("could not install driver: %v", err)
	}
	err = driver.Uninstall()
	if err != nil {
		t.Fatalf("could not uninstall driver: %v", err)
	}
}

func TestDriverDownloadHostEnv(t *testing.T) {
	driverPath := t.TempDir()
	driver, err := NewDriver(&RunOptions{
		DriverDirectory:     driverPath,
		SkipInstallBrowsers: true,
	})
	if err != nil {
		t.Fatalf("could not start driver: %v", err)
	}
	uri := ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uri = r.URL.String()
		w.WriteHeader(404)
	}))
	defer ts.Close()

	err = os.Setenv("PLAYWRIGHT_DOWNLOAD_HOST", ts.URL)
	if err != nil {
		t.Fatalf("could not set PLAYWRIGHT_DOWNLOAD_HOST: %v", err)
	}
	defer os.Unsetenv("PLAYWRIGHT_DOWNLOAD_HOST")
	err = driver.Install()
	if err == nil || !strings.Contains(err.Error(), "404 Not Found") || !strings.Contains(uri, "/builds/driver") {
		t.Fatalf("PLAYWRIGHT_DOWNLOAD_HOST do not work: %v", err)
	}
}

func TestShouldNotHangWhenPlaywrightUnexpectedExit(t *testing.T) {
	if getBrowserName() != "chromium" {
		t.Skip("chromium only")
		return
	}

	pw, err := Run()
	require.NoError(t, err)
	defer func() {
		_ = pw.Stop()
	}()
	browser, err := pw.Chromium.Launch()
	require.NoError(t, err)
	context, err := browser.NewContext()
	require.NoError(t, err)

	err = killPlaywrightProcess()
	require.NoError(t, err)

	_, err = context.NewPage()
	require.Error(t, err)
}

func TestGetNodeExecutable(t *testing.T) {
	// When PLAYWRIGHT_NODEJS_PATH is set, use that path.
	err := os.Setenv("PLAYWRIGHT_NODEJS_PATH", "envDir/node.exe")
	require.NoError(t, err)

	executable := getNodeExecutable("testDirectory")
	assert.Equal(t, "envDir/node.exe", executable)

	err = os.Unsetenv("PLAYWRIGHT_NODEJS_PATH")
	require.NoError(t, err)

	executable = getNodeExecutable("testDirectory")
	assert.Contains(t, executable, "testDirectory")
}

// find and kill playwright process
func killPlaywrightProcess() error {
	all, err := ps.Processes()
	if err != nil {
		return err
	}
	for _, process := range all {
		if process.Executable() == "node" || process.Executable() == "node.exe" {
			if process.PPid() == os.Getpid() {
				if err := killProcessByPid(process.Pid()); err != nil {
					return err
				}
				return nil
			}
		}
	}
	return fmt.Errorf("playwright process not found")
}

func killProcessByPid(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Kill(); err != nil {
		return err
	}
	return nil
}

func getBrowserName() string {
	browserName, hasEnv := os.LookupEnv("BROWSER")
	if hasEnv {
		return browserName
	}
	return "chromium"
}

func readIOAsyncTilEOF(t *testing.T, r *io.PipeReader, wg *sync.WaitGroup, output *string) {
	t.Helper()
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := bufio.NewReader(r)
		for {
			line, _, err := buf.ReadLine()
			if err == io.EOF {
				break
			}
			*output += string(line)
		}
		_ = r.Close()
	}()
}
