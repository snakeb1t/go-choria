package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"sync"

	"github.com/choria-io/go-choria/build"
	"github.com/choria-io/go-choria/choria"
	"github.com/choria-io/go-choria/server"
	"github.com/choria-io/go-config"
	"github.com/choria-io/go-protocol/protocol"
	log "github.com/sirupsen/logrus"
)

type serverCommand struct {
	command
}

type serverRunCommand struct {
	command

	disableTLS       bool
	disableTLSVerify bool
	pidFile          string
}

// server
func (b *serverCommand) Setup() (err error) {
	b.cmd = cli.app.Command("server", "Choria Server")

	return
}

func (b *serverCommand) Run(wg *sync.WaitGroup) (err error) {
	defer wg.Done()

	return
}

func (e *serverCommand) Configure() error {
	cfg.DisableSecurityProviderVerify = true

	return nil
}

// server run
func (r *serverRunCommand) Setup() (err error) {
	if broker, ok := cmdWithFullCommand("server"); ok {
		r.cmd = broker.Cmd().Command("run", "Runs a Choria Server").Default()
		r.cmd.Flag("disable-tls", "Disables TLS").Hidden().Default("false").BoolVar(&r.disableTLS)
		r.cmd.Flag("disable-ssl-verification", "Disables SSL Verification").Hidden().Default("false").BoolVar(&r.disableTLSVerify)
		r.cmd.Flag("pid", "Write running PID to a file").StringVar(&r.pidFile)
	}

	return
}

func (e *serverRunCommand) Configure() error {
	if debug {
		log.SetOutput(os.Stdout)
		log.SetLevel(log.DebugLevel)
		log.Debug("Logging at debug level due to CLI override")
	}

	if choria.FileExist(configFile) {
		cfg, err = config.NewConfig(configFile)
		if err != nil {
			return fmt.Errorf("could not parse configuration: %s", err)
		}
	} else if build.ProvisionBrokerURLs != "" {
		cfg, err = config.NewDefaultConfig()
		if err != nil {
			return fmt.Errorf("could not create default configuration for provisioning: %s", err)
		}
	} else {
		return fmt.Errorf("configuration file %s was not found", configFile)
	}

	cfg.ApplyBuildSettings(&build.Info{})

	cfg.DisableSecurityProviderVerify = true
	cfg.InitiatedByServer = true

	if os.Getenv("INSECURE_YES_REALLY") == "true" {
		protocol.Secure = "false"
		cfg.DisableTLS = true
	}

	return nil
}

func (r *serverRunCommand) Run(wg *sync.WaitGroup) (err error) {
	defer wg.Done()

	if r.disableTLS {
		c.Config.DisableTLS = true
		log.Warn("Running with TLS disabled, not compatible with production use.")
	}

	if r.disableTLSVerify {
		c.Config.DisableTLSVerify = true
		log.Warn("Running with TLS Verification disabled, not compatible with production use.")
	}

	instance, err := server.NewInstance(c)
	if err != nil {
		log.Errorf("Could not start choria: %s", err)
		os.Exit(1)
	}

	log.Infof("Choria Server version %s starting with config %s", build.Version, c.Config.ConfigFile)

	if r.pidFile != "" {
		err := ioutil.WriteFile(r.pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)
		if err != nil {
			return fmt.Errorf("Could not write PID: %s", err)
		}
	}

	wg.Add(1)
	err = instance.Run(ctx, wg)

	return err
}

func init() {
	cli.commands = append(cli.commands, &serverCommand{})
	cli.commands = append(cli.commands, &serverRunCommand{})
}
