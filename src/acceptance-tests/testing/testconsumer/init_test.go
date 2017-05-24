package main_test

import (
	"testing"

	"github.com/onsi/gomega/gexec"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestConsumer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "testing/testconsumer")
}

var pathToConsumer string

var _ = BeforeSuite(func() {
	var err error
	pathToConsumer, err = gexec.Build("github.com/cloudfoundry-incubator/etcd-release/src/acceptance-tests/testing/testconsumer")
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	gexec.CleanupBuildArtifacts()
})
