/*
Copyright 2016 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package main

import (
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gravitational/teleport/lib/defaults"
	"github.com/gravitational/teleport/lib/service"
	"github.com/gravitational/teleport/lib/services"

	"gopkg.in/check.v1"
)

// bootstrap check
func TestTeleportMain(t *testing.T) { check.TestingT(t) }

// register test suite
type MainTestSuite struct {
	hostname   string
	configFile string
}

var _ = check.Suite(&MainTestSuite{})

func (s *MainTestSuite) SetUpSuite(c *check.C) {
	var err error
	// get the hostname
	s.hostname, err = os.Hostname()
	if err != nil {
		panic(err)
	}
	// generate the fixture config file
	dirname, err := ioutil.TempDir("", "teleport")
	if err != nil {
		panic(err)
	}
	s.configFile = filepath.Join(dirname, "teleport.yaml")
	ioutil.WriteFile(s.configFile, []byte(YAMLConfig), 770)

	// set imprtant defaults to test-mode (non-existing files&locations)
	defaults.ConfigFilePath = "/tmp/teleport/etc/teleport.yaml"
	defaults.DataDir = "/tmp/teleport/var/lib/teleport"

	// configure to look for web/dist in the current directory
	curdir, _ := os.Getwd()
	DirsToLookForWebAssets = []string{filepath.Join(curdir, "../../web/dist")}
}

func (s *MainTestSuite) TestDefault(c *check.C) {
	cmd, conf := run([]string{"start"}, true)
	c.Assert(cmd, check.Equals, "start")
	c.Assert(conf.Hostname, check.Equals, s.hostname)
	c.Assert(conf.DataDir, check.Equals, "/tmp/teleport/var/lib/teleport")
	c.Assert(conf.Auth.Enabled, check.Equals, true)
	c.Assert(conf.SSH.Enabled, check.Equals, true)
	c.Assert(conf.Proxy.Enabled, check.Equals, true)
	c.Assert(conf.Console, check.Equals, os.Stdout)
	c.Assert(log.GetLevel(), check.Equals, log.InfoLevel)

	cmd, conf = run([]string{"start", "-d"}, true)
	c.Assert(log.GetLevel(), check.Equals, log.DebugLevel)
}

func (s *MainTestSuite) TestRolesFlag(c *check.C) {
	cmd, conf := run([]string{"start", "--roles=node"}, true)
	c.Assert(conf.SSH.Enabled, check.Equals, true)
	c.Assert(conf.Auth.Enabled, check.Equals, false)
	c.Assert(conf.Proxy.Enabled, check.Equals, false)

	cmd, conf = run([]string{"start", "--roles=proxy"}, true)
	c.Assert(conf.SSH.Enabled, check.Equals, false)
	c.Assert(conf.Auth.Enabled, check.Equals, false)
	c.Assert(conf.Proxy.Enabled, check.Equals, true)

	cmd, conf = run([]string{"start", "--roles=auth"}, true)
	c.Assert(conf.SSH.Enabled, check.Equals, false)
	c.Assert(conf.Auth.Enabled, check.Equals, true)
	c.Assert(conf.Proxy.Enabled, check.Equals, false)
	c.Assert(cmd, check.Equals, "start")
}

func (s *MainTestSuite) TestConfigFile(c *check.C) {
	cmd, conf := run([]string{"start", "--roles=node", "-d", "--labels=a=a1,b=b1", "--config=" + s.configFile}, true)
	c.Assert(cmd, check.Equals, "start")
	c.Assert(conf.SSH.Enabled, check.Equals, true)
	c.Assert(conf.Auth.Enabled, check.Equals, false)
	c.Assert(conf.Proxy.Enabled, check.Equals, false)
	c.Assert(log.GetLevel(), check.Equals, log.DebugLevel)
	c.Assert(conf.Hostname, check.Equals, "hvostongo.example.org")
	c.Assert(conf.SSH.Token, check.Equals, "xxxyyy")
	c.Assert(conf.AdvertiseIP, check.DeepEquals, net.ParseIP("10.5.5.5"))
	c.Assert(conf.SSH.Labels, check.DeepEquals, map[string]string{"a": "a1", "b": "b1"})
}

func (s *MainTestSuite) TestLabelParsing(c *check.C) {
	var conf service.SSHConfig
	var err error
	// empty spec. no errors, no labels
	err = parseLabels("", &conf)
	c.Assert(err, check.IsNil)
	c.Assert(conf.CmdLabels, check.IsNil)
	c.Assert(conf.Labels, check.IsNil)

	// simple static labels
	err = parseLabels(`key=value,more="much better"`, &conf)
	c.Assert(err, check.IsNil)
	c.Assert(conf.CmdLabels, check.NotNil)
	c.Assert(conf.CmdLabels, check.HasLen, 0)
	c.Assert(conf.Labels, check.DeepEquals, map[string]string{
		"key":  "value",
		"more": "much better",
	})

	// static labels + command labels
	err = parseLabels(`key=value,more="much better",arch=[5m2s:/bin/uname -m "p1 p2"]`, &conf)
	c.Assert(err, check.IsNil)
	c.Assert(conf.Labels, check.DeepEquals, map[string]string{
		"key":  "value",
		"more": "much better",
	})
	c.Assert(conf.CmdLabels, check.DeepEquals, services.CommandLabels{
		"arch": services.CommandLabel{
			Period:  time.Minute*5 + time.Second*2,
			Command: []string{"/bin/uname", "-m", `"p1 p2"`},
		},
	})
}

func (s *MainTestSuite) TestLocateWebAssets(c *check.C) {
	path, err := locateWebAssets()
	c.Assert(path, check.Equals, DirsToLookForWebAssets[0])
	c.Assert(err, check.IsNil)

	origDirs := DirsToLookForWebAssets
	defer func() {
		DirsToLookForWebAssets = origDirs
	}()
	DirsToLookForWebAssets = []string{"/bad/dir"}
	path, err = locateWebAssets()
	c.Assert(path, check.Equals, "")
	c.Assert(err, check.NotNil)
}

const YAMLConfig = `
teleport:
  advertise_ip: 10.5.5.5
  nodename: hvostongo.example.org
  auth_servers:
    - tcp://auth.server.example.org:3024
  auth_token: xxxyyy
  log:
    output: stderr
    severity: DEBUG
  connection_limits:
    max_connections: 90
    max_users: 91
    rates:
    - period: 1m1s
      average: 70
      burst: 71
    - period: 10m10s
      average: 170
      burst: 171

auth_service:
  enabled: yes
  listen_addr: tcp://auth

ssh_service:
  enabled: no
  listen_addr: tcp://ssh
  labels:
    name: mondoserver
    role: slave
  commands:
  - name: hostname
    command: [/bin/hostname]
    period: 10ms
  - name: date
    command: [/bin/date]
    period: 20ms
`
