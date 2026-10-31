//go:build windows

package cloudsync

import (
	"fmt"
	"golang.org/x/sys/windows"
	"unsafe"
)

var advapi = windows.NewLazySystemDLL("advapi32.dll")
var credRead = advapi.NewProc("CredReadW")
var credWrite = advapi.NewProc("CredWriteW")
var credDelete = advapi.NewProc("CredDeleteW")
var credFree = advapi.NewProc("CredFree")

type credential struct {
	Flags          uint32
	Type           uint32
	TargetName     *uint16
	Comment        *uint16
	LastWritten    windows.Filetime
	BlobSize       uint32
	Blob           *byte
	Persist        uint32
	AttributeCount uint32
	Attributes     uintptr
	TargetAlias    *uint16
	UserName       *uint16
}

func target(account string) *uint16 {
	p, _ := windows.UTF16PtrFromString("LorisTunnel:CloudSync:" + account)
	return p
}
func (SystemVault) Get(account string) (string, error) {
	var p *credential
	r, _, e := credRead.Call(uintptr(unsafe.Pointer(target(account))), 1, 0, uintptr(unsafe.Pointer(&p)))
	if r == 0 {
		return "", fmt.Errorf("Credential Manager read failed: %w", e)
	}
	defer credFree.Call(uintptr(unsafe.Pointer(p)))
	return string(unsafe.Slice(p.Blob, p.BlobSize)), nil
}
func (SystemVault) Set(account, token string) error {
	b := []byte(token)
	if len(b) == 0 {
		return fmt.Errorf("empty token")
	}
	c := credential{Type: 1, TargetName: target(account), BlobSize: uint32(len(b)), Blob: &b[0], Persist: 2}
	r, _, e := credWrite.Call(uintptr(unsafe.Pointer(&c)), 0)
	if r == 0 {
		return fmt.Errorf("Credential Manager write failed: %w", e)
	}
	return nil
}
func (SystemVault) Delete(account string) error {
	r, _, e := credDelete.Call(uintptr(unsafe.Pointer(target(account))), 1, 0)
	if r == 0 && e != windows.ERROR_NOT_FOUND {
		return fmt.Errorf("Credential Manager delete failed: %w", e)
	}
	return nil
}
