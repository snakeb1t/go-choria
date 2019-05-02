package aagent

import (
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choria-io/go-choria/aagent/machine"
	notifier "github.com/choria-io/go-choria/aagent/notifiers/choria"
	"github.com/choria-io/go-choria/aagent/watchers"
	"github.com/choria-io/go-choria/choria"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type AAgent struct {
	fw       ChoriaProvider
	logger   *logrus.Entry
	machines []*managedMachine
	notifier *notifier.Notifier

	source string

	sync.Mutex
}

type managedMachine struct {
	path       string
	loaded     time.Time
	machine    *machine.Machine
	loadedHash string
}

// ChoriaProvider provides access to the choria framework
type ChoriaProvider interface {
	PublishRaw(string, []byte) error
	Logger(string) *logrus.Entry
	Identity() string
}

// New creates a new instance of the choria autonomous agent host
func New(dir string, fw ChoriaProvider) (aa *AAgent, err error) {
	notifier, err := notifier.New(fw)
	if err != nil {
		return nil, errors.Wrapf(err, "could not create notifier")
	}

	return &AAgent{
		fw:       fw,
		logger:   fw.Logger("aagent"),
		source:   dir,
		machines: []*managedMachine{},
		notifier: notifier,
	}, nil
}

// ManageMachines start observing the
func (a *AAgent) ManageMachines(ctx context.Context, wg *sync.WaitGroup) error {
	wg.Add(1)
	go a.watchSource(ctx, wg)

	return nil
}

func (a *AAgent) loadMachine(ctx context.Context, wg *sync.WaitGroup, path string) (err error) {
	aa, err := machine.FromDir(path, watchers.New())
	if err != nil {
		return err
	}

	sum, err := aa.Hash()
	if err != nil {
		return err
	}

	a.logger.Infof("Loaded Autonomous Agent %s version %s from %s (%s)", aa.Name(), aa.Version(), path, sum)

	aa.SetIdentity(a.fw.Identity())
	aa.RegisterNotifier(a.notifier)

	managed := &managedMachine{
		loaded:     time.Now(),
		path:       path,
		machine:    aa,
		loadedHash: sum,
	}

	a.Lock()
	a.machines = append(a.machines, managed)
	a.Unlock()

	aa.Start(ctx, wg)

	return nil
}

func (a *AAgent) loadFromSource(ctx context.Context, wg *sync.WaitGroup) error {
	files, err := ioutil.ReadDir(a.source)
	if err != nil {
		return errors.Wrapf(err, "could not read machine source")
	}

	for _, file := range files {
		path := filepath.Join(a.source, file.Name())

		if !file.IsDir() || strings.HasPrefix(path, ".") {
			continue
		}

		current := a.findMachine("", "", path)

		if current != nil {
			hash, err := current.machine.Hash()
			if err != nil {
				a.logger.Errorf("could not determine hash for %s manifest in %s")
			}

			if hash == current.loadedHash {
				continue
			}

			a.logger.Warnf("Loaded machine %s does not match current manifest (%s), stopping", current.machine.Name(), hash)
			current.machine.Stop()
			err = a.deleteByPath(path)
			if err != nil {
				a.logger.Errorf("could not delete machine for %s", path)
			}
			a.logger.Debugf("Sleeping 1 second to allow old machine to exit")
			choria.InterruptableSleep(ctx, time.Second)
		}

		a.logger.Infof("Attempting to load Choria Machine from %s", path)

		err = a.loadMachine(ctx, wg, path)
		if err != nil {
			a.logger.Errorf("Could not load machine from %s: %s", path, err)
		}
	}

	return nil
}

func (a *AAgent) watchSource(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	tick := time.NewTicker(10 * time.Second)

	loadf := func() {
		err := a.loadFromSource(ctx, wg)
		if err != nil {
			a.logger.Errorf("Could not load Autonomous Agents: %s", err)
		}
	}

	loadf()

	for {
		select {
		case <-tick.C:
			loadf()

		case <-ctx.Done():
			return
		}
	}
}

func (a *AAgent) deleteByPath(path string) error {
	a.Lock()
	defer a.Unlock()

	match := -1

	for i, m := range a.machines {
		if m.path == path {
			match = i
		}
	}

	if match >= 0 {
		// delete without memleaks, apparently, https://github.com/golang/go/wiki/SliceTricks
		a.machines[match] = a.machines[len(a.machines)-1]
		a.machines[len(a.machines)-1] = nil
		a.machines = a.machines[:len(a.machines)-1]

		return nil
	}

	return fmt.Errorf("could not find a machine from %s", path)
}

func (a *AAgent) findMachine(name string, version string, path string) *managedMachine {
	a.Lock()
	defer a.Unlock()

	if name == "" && version == "" && path == "" {
		return nil
	}

	for _, m := range a.machines {
		nameMatch := name == ""
		versionMatch := version == ""
		pathMatch := path == ""

		if name != "" {
			nameMatch = m.machine.Name() == name
		}

		if path != "" {
			pathMatch = m.path == path
		}

		if version != "" {
			versionMatch = m.machine.Version() == version
		}

		if nameMatch && versionMatch && pathMatch {
			return m
		}
	}

	return nil
}
