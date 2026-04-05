package paa_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestProtectiveAssetAllocation(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Protective Asset Allocation Suite")
}
