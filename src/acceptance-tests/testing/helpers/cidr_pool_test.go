package helpers_test

import (
	"acceptance-tests/testing/helpers"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("CIDRPool", func() {
	var (
		cidrPool helpers.CIDRPool
	)

	Describe("Get", func() {
		It("returns a valid cidr block from the cidr pool", func() {
			cidrPool = helpers.NewCIDRPool("10.244.4.0", 24, 26)

			cidr, err := cidrPool.Get(0)
			Expect(err).NotTo(HaveOccurred())
			Expect(cidr).To(Equal("10.244.4.0/26"))

			cidr, err = cidrPool.Get(1)
			Expect(err).NotTo(HaveOccurred())
			Expect(cidr).To(Equal("10.244.4.64/26"))

			cidr, err = cidrPool.Get(2)
			Expect(err).NotTo(HaveOccurred())
			Expect(cidr).To(Equal("10.244.4.128/26"))

			cidr, err = cidrPool.Get(3)
			Expect(err).NotTo(HaveOccurred())
			Expect(cidr).To(Equal("10.244.4.192/26"))
		})

		Context("failure cases", func() {
			It("returns an error if it attempts to get with an index higher than the pool size", func() {
				cidrPool = helpers.NewCIDRPool("10.244.4.0", 24, 26)

				_, err := cidrPool.Get(4)
				Expect(err).To(MatchError("cannot get cidr of index 4 when pool size is size of 4"))
			})
		})
	})
})
