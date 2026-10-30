package store_test

import (
	"testing"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

func testIOSDurableRetry(t *testing.T, repo repository, reopen func() repository) {
	material := testMaterial(t)
	client := material.Recovery.Client
	client.Platform = protocolv1.ClientPlatform_CLIENT_PLATFORM_IOS
	client.AppVersion, client.Manufacturer, client.Device = "2.26.40.77", "Apple", "iPhone12,3"
	testDurableRetryWithMaterial(t, repo, reopen, material)
}
