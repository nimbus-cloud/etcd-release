package main_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"

	"testing"
)

func TestEtcdCC(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "etcd-consistency-checker")
}

var (
	pathToEtcdCC string
)

var _ = BeforeSuite(func() {
	var err error

	pathToEtcdCC, err = gexec.Build("etcd-consistency-checker")
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	gexec.CleanupBuildArtifacts()
})
