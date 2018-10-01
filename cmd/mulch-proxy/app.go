package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path"
	"strconv"
	"syscall"
)

// App describes an the application
type App struct {
	Config      *AppConfig
	Log         *Log
	ProxyServer *ProxyServer
}

// NewApp creates a new application
func NewApp(config *AppConfig, trace bool) (*App, error) {
	app := &App{
		Config: config,
		Log:    NewLog(trace),
	}

	app.Log.Trace("starting application")

	err := app.checkDataPath()
	if err != nil {
		return nil, err
	}

	ddb, err := app.createDomainDB()
	if err != nil {
		return nil, err
	}

	cacheDir, err := app.initCertCache()
	if err != nil {
		return nil, err
	}

	app.ProxyServer = NewProxyServer(
		cacheDir,
		app.Config.AcmeEmail,
		app.Config.HTTPAddress,
		app.Config.HTTPSAddress,
		app.Config.AcmeURL,
		ddb,
		app.Log,
	)

	app.ProxyServer.RefreshReverseProxies()

	app.initSigHUPHandler()

	return app, nil
}

func (app *App) checkDataPath() error {
	if _, err := os.Stat(app.Config.DataPath); os.IsNotExist(err) {
		return fmt.Errorf("data path (%s) does not exist", app.Config.DataPath)
	}
	lastPidFilename := path.Clean(app.Config.DataPath + "/mulch-proxy-last.pid")
	pid := os.Getpid()
	ioutil.WriteFile(lastPidFilename, []byte(strconv.Itoa(pid)), 0644)
	return nil
}

func (app *App) createDomainDB() (*DomainDatabase, error) {
	dbPath := path.Clean(app.Config.DataPath + "/mulch-proxy-domains.db")

	ddb, err := NewDomainDatabase(dbPath)
	if err != nil {
		return nil, err
	}

	app.Log.Infof("found %d domain(s) in database %s", ddb.Count(), dbPath)

	return ddb, nil
}

func (app *App) initCertCache() (string, error) {
	cacheDir := path.Clean(app.Config.DataPath + "/certs")

	stat, err := os.Stat(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			app.Log.Infof("%s does not exists, let's create it", cacheDir)
			errM := os.Mkdir(cacheDir, 0700)
			if errM != nil {
				return "", errM
			}
			return cacheDir, nil
		}
		return "", err
	}

	if stat.IsDir() == false {
		return "", fmt.Errorf("%s is not a directory", cacheDir)
	}

	if stat.Mode() != os.ModeDir|os.FileMode(0700) {
		fmt.Println(stat.Mode())
		return "", fmt.Errorf("%s: only the owner should be able to read/write this directory (mode 0700)", cacheDir)
	}

	return cacheDir, nil
}

func (app *App) initSigHUPHandler() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)

	go func() {
		for sig := range c {
			if sig == syscall.SIGHUP {
				app.Log.Infof("HUP Signal, reloading domains")
				app.ProxyServer.ReloadDomains()
			}
		}
	}()
}

// Run will start the app (in the foreground)
func (app *App) Run() {
	app.Log.Info("running proxy…")
	err := app.ProxyServer.Run()
	if err != nil {
		app.Log.Error(err.Error())
		app.Log.Info("For 'bind: permission denied' on lower ports, you may use setcap:")
		app.Log.Info("Ex: setcap 'cap_net_bind_service=+ep' mulch-proxy")
		os.Exit(99)
	}
}