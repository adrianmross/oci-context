//go:build darwin && cgo

package cmd

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <stdlib.h>

static CFStringRef keychainString(const char *value) {
	return CFStringCreateWithCString(NULL, value, kCFStringEncodingUTF8);
}

static OSStatus keychainRead(const char *service, const char *account, unsigned char **bytes, CFIndex *length) {
	CFStringRef serviceValue = keychainString(service);
	CFStringRef accountValue = keychainString(account);
	const void *keys[] = { kSecClass, kSecAttrService, kSecAttrAccount, kSecReturnData, kSecMatchLimit };
	const void *values[] = { kSecClassGenericPassword, serviceValue, accountValue, kCFBooleanTrue, kSecMatchLimitOne };
	CFDictionaryRef query = CFDictionaryCreate(NULL, keys, values, 5, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFTypeRef result = NULL;
	OSStatus status = SecItemCopyMatching(query, &result);
	if (status == errSecSuccess) {
		CFDataRef data = (CFDataRef)result;
		*length = CFDataGetLength(data);
		*bytes = malloc(*length);
		if (*bytes == NULL) {
			status = errSecAllocate;
		} else {
			CFDataGetBytes(data, CFRangeMake(0, *length), *bytes);
		}
		CFRelease(result);
	}
	CFRelease(query);
	CFRelease(accountValue);
	CFRelease(serviceValue);
	return status;
}

static OSStatus keychainWrite(const char *service, const char *account, const unsigned char *bytes, CFIndex length) {
	CFStringRef serviceValue = keychainString(service);
	CFStringRef accountValue = keychainString(account);
	CFDataRef dataValue = CFDataCreate(NULL, bytes, length);
	const void *queryKeys[] = { kSecClass, kSecAttrService, kSecAttrAccount };
	const void *queryValues[] = { kSecClassGenericPassword, serviceValue, accountValue };
	CFDictionaryRef query = CFDictionaryCreate(NULL, queryKeys, queryValues, 3, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	const void *attributesKeys[] = { kSecValueData };
	const void *attributesValues[] = { dataValue };
	CFDictionaryRef attributes = CFDictionaryCreate(NULL, attributesKeys, attributesValues, 1, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	OSStatus status = SecItemUpdate(query, attributes);
	if (status == errSecItemNotFound) {
		const void *addKeys[] = { kSecClass, kSecAttrService, kSecAttrAccount, kSecValueData };
		const void *addValues[] = { kSecClassGenericPassword, serviceValue, accountValue, dataValue };
		CFDictionaryRef item = CFDictionaryCreate(NULL, addKeys, addValues, 4, &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
		status = SecItemAdd(item, NULL);
		if (status == errSecDuplicateItem) status = SecItemUpdate(query, attributes);
		CFRelease(item);
	}
	CFRelease(attributes);
	CFRelease(query);
	CFRelease(dataValue);
	CFRelease(accountValue);
	CFRelease(serviceValue);
	return status;
}
*/
import "C"

import (
	"fmt"
	"os"
	"unsafe"
)

func authTokenKeychainAvailable() bool { return true }

func readAuthTokenKeychain(service, account string) ([]byte, error) {
	serviceValue := C.CString(service)
	accountValue := C.CString(account)
	defer C.free(unsafe.Pointer(serviceValue))
	defer C.free(unsafe.Pointer(accountValue))
	var bytes *C.uchar
	var length C.CFIndex
	if status := C.keychainRead(serviceValue, accountValue, &bytes, &length); status != C.errSecSuccess {
		if status == C.errSecItemNotFound {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("read OAuth token from Keychain: status %d", int(status))
	}
	defer C.free(unsafe.Pointer(bytes))
	return C.GoBytes(unsafe.Pointer(bytes), C.int(length)), nil
}

func writeAuthTokenKeychain(service, account string, data []byte) error {
	serviceValue := C.CString(service)
	accountValue := C.CString(account)
	defer C.free(unsafe.Pointer(serviceValue))
	defer C.free(unsafe.Pointer(accountValue))
	if status := C.keychainWrite(serviceValue, accountValue, (*C.uchar)(unsafe.Pointer(&data[0])), C.CFIndex(len(data))); status != C.errSecSuccess {
		return fmt.Errorf("write OAuth token to Keychain: status %d", int(status))
	}
	return nil
}
