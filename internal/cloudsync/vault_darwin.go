//go:build darwin && cgo

package cloudsync

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <stdlib.h>
#include <string.h>
static CFMutableDictionaryRef vault_query(char *account) {
 CFMutableDictionaryRef query=CFDictionaryCreateMutable(
    NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks
  );
 CFStringRef name=CFStringCreateWithCString(NULL,account,kCFStringEncodingUTF8);
 CFDictionarySetValue(query,kSecClass,kSecClassGenericPassword);
 CFDictionarySetValue(query,kSecAttrService,CFSTR("cc.lorisdev.tunnel.cloud-sync"));
 CFDictionarySetValue(query,kSecAttrAccount,name);
 CFRelease(name);
 return query;
}
static OSStatus vault_get(char *account, char **data, long *length) {
 CFMutableDictionaryRef query=vault_query(account);
 CFDictionarySetValue(query,kSecReturnData,kCFBooleanTrue);
 CFDictionarySetValue(query,kSecMatchLimit,kSecMatchLimitOne);
 CFTypeRef result=NULL;
 OSStatus status=SecItemCopyMatching(query,&result);
 CFRelease(query);
 if(status!=errSecSuccess)return status;
 *length=CFDataGetLength((CFDataRef)result);
 *data=malloc(*length);
 if(*length>0&&!*data){CFRelease(result);return errSecAllocate;}
 memcpy(*data,CFDataGetBytePtr((CFDataRef)result),*length);
 CFRelease(result);return status;
}
static OSStatus vault_set(char *account, char *token) {
 CFMutableDictionaryRef query=vault_query(account);
 CFDataRef data=CFDataCreate(NULL,(const UInt8*)token,strlen(token));
 CFMutableDictionaryRef update=CFDictionaryCreateMutable(
    NULL, 0, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks
  );
 CFDictionarySetValue(update,kSecValueData,data);
 OSStatus status=SecItemUpdate(query,update);
 if(status==errSecItemNotFound){CFDictionarySetValue(query,kSecValueData,data);status=SecItemAdd(query,NULL);}
 CFRelease(data);CFRelease(update);CFRelease(query);return status;
}
static OSStatus vault_delete(char *account) {
 CFMutableDictionaryRef query=vault_query(account);
 OSStatus status=SecItemDelete(query);CFRelease(query);
 return status==errSecItemNotFound?errSecSuccess:status;
}
*/
import "C"
import (
	"fmt"
	"unsafe"
)

func (SystemVault) Get(account string) (string, error) {
	a := C.CString(account)
	defer C.free(unsafe.Pointer(a))
	var n C.long
	var p *C.char
	s := C.vault_get(a, &p, &n)
	if s != 0 {
		return "", fmt.Errorf("Keychain read failed (%d)", int(s))
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoStringN(p, C.int(n)), nil
}
func (SystemVault) Set(account, token string) error {
	a, t := C.CString(account), C.CString(token)
	defer C.free(unsafe.Pointer(a))
	defer C.free(unsafe.Pointer(t))
	s := C.vault_set(a, t)
	if s != 0 {
		return fmt.Errorf("Keychain write failed (%d)", int(s))
	}
	return nil
}
func (SystemVault) Delete(account string) error {
	a := C.CString(account)
	defer C.free(unsafe.Pointer(a))
	s := C.vault_delete(a)
	if s != 0 {
		return fmt.Errorf("Keychain delete failed (%d)", int(s))
	}
	return nil
}
