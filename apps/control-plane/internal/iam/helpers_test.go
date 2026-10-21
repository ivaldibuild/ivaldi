package iam

import (
	"crypto/sha256"
	"time"
)

// Shared fixtures for the domain tests.
var (
	t0 = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Hour)
	t2 = t0.Add(2 * time.Hour)
)

const (
	testOrg       = "org_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	otherOrg      = "org_01BX5ZZKBKACTAV9WEVGEMMVRZ"
	testAccount   = "acc_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	testPrincipal = "prn_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	testCreator   = "prn_01BX5ZZKBKACTAV9WEVGEMMVRZ"
	testIdP       = "idp_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	testClient    = "ocl_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	testGroup     = "grp_01ARZ3NDEKTSV4RRFFQ69G5FAV"
	testWorkspace = "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV"
)

// testHash returns the SHA-256 hash of s, standing in for a hashed token.
func testHash(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}
