//go:build linux && amd64

package main

import (
	"strconv"
	"strings"
	"testing"
)

func cloneBStatus(uid, gid int) []byte {
	return []byte(strings.Join([]string{
		"Name:\tsymmetry-linux-clone-bootstrap",
		"Uid:\t" + strconv.Itoa(uid) + "\t" + strconv.Itoa(uid) + "\t" + strconv.Itoa(uid) + "\t" + strconv.Itoa(uid),
		"Gid:\t" + strconv.Itoa(gid) + "\t" + strconv.Itoa(gid) + "\t" + strconv.Itoa(gid) + "\t" + strconv.Itoa(gid),
		"Groups:",
		"CapInh:\t0000000000000000",
		"CapPrm:\t0000000000000000",
		"CapEff:\t0000000000000000",
		"CapBnd:\t0000000000000000",
		"CapAmb:\t0000000000000000",
		"NoNewPrivs:\t1",
	}, "\n"))
}

func TestValidateBStatusAcceptsFixedNamespaceIdentity(t *testing.T) {
	if err := validateBStatus(cloneBStatus(1, 1)); err != nil {
		t.Fatalf("fixed namespace B status was rejected: %v", err)
	}
}

func TestValidateBStatusRejectsHostIdentity(t *testing.T) {
	if err := validateBStatus(cloneBStatus(1003, 1004)); err == nil {
		t.Fatal("host B UID/GID was accepted as namespace-local identity")
	}
}

func TestValidateBStatusRejectsSupplementaryGroups(t *testing.T) {
	status := strings.Replace(string(cloneBStatus(1, 1)), "Groups:", "Groups:\t1004", 1)
	if err := validateBStatus([]byte(status)); err == nil {
		t.Fatal("supplementary groups were accepted")
	}
}
