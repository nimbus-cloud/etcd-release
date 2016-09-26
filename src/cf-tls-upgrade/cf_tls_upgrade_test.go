package cf_tls_upgrade_test

import (
	"acceptance-tests/testing/helpers"
	"cf-tls-upgrade/logspammer"
	"cf-tls-upgrade/syslogchecker"
	"crypto/tls"
	"fmt"
	"math/rand"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cloudfoundry-incubator/cf-test-helpers/cf"
	"github.com/cloudfoundry-incubator/cf-test-helpers/generator"
	"github.com/cloudfoundry/noaa/consumer"
	"github.com/onsi/gomega/gexec"
	"github.com/pivotal-cf-experimental/bosh-test/bosh"
	"gopkg.in/yaml.v2"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	CF_PUSH_TIMEOUT                = 2 * time.Minute
	DEFAULT_TIMEOUT                = 30 * time.Second
	GUID_NOT_FOUND_ERROR_THRESHOLD = 1
)

type gen struct{}

func (gen) Generate() string {
	return strconv.Itoa(rand.Int())
}

func getNonErrandVMsFromRawManifest(rawManifest []byte) ([]bosh.VM, error) {
	var vms []bosh.VM

	var manifest helpers.Manifest
	err := yaml.Unmarshal(rawManifest, &manifest)
	if err != nil {
		return nil, err
	}

	for _, job := range manifest.Jobs {
		for i := 0; i < job.Instances; i++ {
			if job.Lifecycle != "errand" {
				vms = append(vms, bosh.VM{JobName: job.Name, Index: i, State: "running"})
			}
		}
	}

	return vms, nil
}

type runner struct{}

func (runner) Run(args ...string) ([]byte, error) {
	return exec.Command("cf", args...).CombinedOutput()
}

var _ = Describe("CF TLS Upgrade Test", func() {
	It("successfully upgrades etcd cluster to use TLS", func() {
		var (
			migrationManifest []byte
			err               error
			appName           string
			spammer           *logspammer.Spammer
			checker           syslogchecker.Checker
		)

		var getToken = func() string {
			session := cf.Cf("oauth-token")
			Eventually(session, DEFAULT_TIMEOUT).Should(gexec.Exit(0))

			token := strings.TrimSpace(string(session.Out.Contents()))
			Expect(token).NotTo(Equal(""))
			return token
		}

		var getAppGuid = func(appName string) string {
			cfApp := cf.Cf("app", appName, "--guid")
			Eventually(cfApp, DEFAULT_TIMEOUT).Should(gexec.Exit(0))

			appGuid := strings.TrimSpace(string(cfApp.Out.Contents()))
			Expect(appGuid).NotTo(Equal(""))
			return appGuid
		}

		var enableDiego = func(appName string) {
			guid := getAppGuid(appName)
			Eventually(cf.Cf("curl", "/v2/apps/"+guid, "-X", "PUT", "-d", `{"diego": true}`), DEFAULT_TIMEOUT).Should(gexec.Exit(0))
		}

		By("logging into cf and preparing the environment", func() {
			cfConfig := config.CF
			Eventually(
				cf.Cf("login", "-a", fmt.Sprintf("api.%s", cfConfig.Domain),
					"-u", cfConfig.Username, "-p", cfConfig.Password,
					"--skip-ssl-validation"),
				DEFAULT_TIMEOUT).Should(gexec.Exit(0))

			Eventually(cf.Cf("create-org", "EATS_org"), DEFAULT_TIMEOUT).Should(gexec.Exit(0))
			Eventually(cf.Cf("target", "-o", "EATS_org"), DEFAULT_TIMEOUT).Should(gexec.Exit(0))

			Eventually(cf.Cf("create-space", "EATS_space"), DEFAULT_TIMEOUT).Should(gexec.Exit(0))
			Eventually(cf.Cf("target", "-s", "EATS_space"), DEFAULT_TIMEOUT).Should(gexec.Exit(0))

			Eventually(cf.Cf("enable-feature-flag", "diego_docker"), DEFAULT_TIMEOUT).Should(gexec.Exit(0))
		})

		By("pushing an application to diego", func() {
			appName = generator.PrefixedRandomName("EATS-APP-")
			Eventually(cf.Cf(
				"push", appName,
				"-f", "assets/logspinner/manifest.yml",
				"--no-start"),
				CF_PUSH_TIMEOUT).Should(gexec.Exit(0))

			enableDiego(appName)

			Eventually(cf.Cf("start", appName), CF_PUSH_TIMEOUT).Should(gexec.Exit(0))
		})

		By("starting the syslog-drain process", func() {
			syslogAppName := generator.PrefixedRandomName("syslog-source-app-")
			Eventually(cf.Cf(
				"push", syslogAppName,
				"-f", "assets/logspinner/manifest.yml",
				"--no-start"),
				CF_PUSH_TIMEOUT).Should(gexec.Exit(0))

			enableDiego(syslogAppName)

			Eventually(cf.Cf("start", syslogAppName), CF_PUSH_TIMEOUT).Should(gexec.Exit(0))
			checker = syslogchecker.New("syslog-drainer", gen{}, 1*time.Millisecond, runner{})
			checker.Start(syslogAppName, fmt.Sprintf("http://%s.%s", syslogAppName, config.CF.Domain))
		})

		By("spamming logs", func() {
			consumer := consumer.New(fmt.Sprintf("wss://doppler.%s:4443", config.CF.Domain), &tls.Config{InsecureSkipVerify: true}, nil)
			msgChan, _ := consumer.Stream(getAppGuid(appName), getToken())
			spammer = logspammer.NewSpammer(fmt.Sprintf("http://%s.%s", appName, config.CF.Domain), msgChan, 10*time.Millisecond)
			Eventually(func() bool {
				return spammer.CheckStream()
			}).Should(BeTrue())

			err = spammer.Start()
			Expect(err).NotTo(HaveOccurred())
		})

		By("scaling down the non-TLS etcd cluster to 1 node and converting it to a proxy", func() {
			originalManifest, err := client.DownloadManifest(config.BOSH.DeploymentName)
			Expect(err).NotTo(HaveOccurred())

			migrationManifest, err = helpers.CreateCFTLSMigrationManifest(originalManifest)
			Expect(err).NotTo(HaveOccurred())

			_, err = client.Deploy(migrationManifest)
			Expect(err).NotTo(HaveOccurred())
		})

		By("checking if expected VMs are running", func() {
			expectedVMs, err := getNonErrandVMsFromRawManifest(migrationManifest)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() ([]bosh.VM, error) {
				return client.DeploymentVMs(config.BOSH.DeploymentName)
			}, "1m", "10s").Should(ConsistOf(expectedVMs))
		})

		By("deploy diego to switch clients to tls etcd", func() {
			deploymentName := fmt.Sprintf("%s-diego", config.BOSH.DeploymentName)
			rawManifest, err := client.DownloadManifest(deploymentName)
			Expect(err).NotTo(HaveOccurred())

			manifest, err := helpers.CreateDiegoTLSMigrationManifest(rawManifest)
			Expect(err).NotTo(HaveOccurred())

			_, err = client.Deploy(manifest)
			Expect(err).NotTo(HaveOccurred())

			expectedVMs, err := getNonErrandVMsFromRawManifest(manifest)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() ([]bosh.VM, error) {
				return client.DeploymentVMs(deploymentName)
			}, "1m", "10s").Should(ConsistOf(expectedVMs))
		})

		By("running a couple iterations of the syslog-drain checker", func() {
			count := checker.GetIterationCount()
			Eventually(checker.GetIterationCount, "10m", "10s").Should(BeNumerically(">", count+2))
		})

		By("stopping spammer and checking for errors", func() {
			err = spammer.Stop()
			Expect(err).NotTo(HaveOccurred())

			err = spammer.Check()
			Expect(err).NotTo(HaveOccurred())
		})

		By("stopping syslogchecker and checking for errors", func() {
			err = checker.Stop()
			Expect(err).NotTo(HaveOccurred())

			spammerErrs := checker.Check()

			if spammerErrs == nil {
				return
			}

			var errorSet helpers.ErrorSet

			switch spammerErrs.(type) {
			case helpers.ErrorSet:
				errorSet = spammerErrs.(helpers.ErrorSet)
			default:
				Fail(spammerErrs.Error())
			}

			Expect(errorSet["could not validate the guid on syslog"]).To(BeNumerically("<=", GUID_NOT_FOUND_ERROR_THRESHOLD))
			delete(errorSet, "could not validate the guid on syslog")

			Expect(errorSet).To(HaveLen(0))
		})
	})
})
