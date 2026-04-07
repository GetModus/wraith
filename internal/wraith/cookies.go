package wraith

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// macEpoch is 2001-01-01 00:00:00 UTC — Apple's reference date.
var macEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// Cookie represents a parsed Safari cookie.
type Cookie struct {
	Name     string
	Value    string
	Domain   string
	Path     string
	Expires  time.Time
	Secure   bool
	HTTPOnly bool
}

// DefaultCookiePath returns the path to Safari's binary cookie store.
func DefaultCookiePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Containers", "com.apple.Safari",
		"Data", "Library", "Cookies", "Cookies.binarycookies")
}

// ParseBinaryCookies reads Safari's Cookies.binarycookies file.
func ParseBinaryCookies(path string) ([]Cookie, error) {
	if path == "" {
		path = DefaultCookiePath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read cookie file: %w", err)
	}

	if len(data) < 8 || string(data[:4]) != "cook" {
		return nil, fmt.Errorf("invalid binarycookies magic")
	}

	numPages := binary.BigEndian.Uint32(data[4:8])

	// Read page sizes (big-endian uint32 array)
	pageSizes := make([]uint32, numPages)
	for i := uint32(0); i < numPages; i++ {
		offset := 8 + i*4
		if int(offset+4) > len(data) {
			break
		}
		pageSizes[i] = binary.BigEndian.Uint32(data[offset : offset+4])
	}

	// Pages start after header
	pageOffset := 8 + numPages*4
	var cookies []Cookie

	for _, size := range pageSizes {
		if int(pageOffset+size) > len(data) {
			break
		}
		pageData := data[pageOffset : pageOffset+size]
		pageCookies := parsePage(pageData)
		cookies = append(cookies, pageCookies...)
		pageOffset += size
	}

	return cookies, nil
}

func parsePage(data []byte) []Cookie {
	if len(data) < 8 {
		return nil
	}

	// Page header: big-endian 0x00000100
	header := binary.BigEndian.Uint32(data[0:4])
	if header != 0x00000100 {
		return nil
	}

	numCookies := binary.LittleEndian.Uint32(data[4:8])
	var cookies []Cookie

	for i := uint32(0); i < numCookies; i++ {
		offsetPos := 8 + i*4
		if int(offsetPos+4) > len(data) {
			break
		}
		cookieOffset := binary.LittleEndian.Uint32(data[offsetPos : offsetPos+4])
		if int(cookieOffset) >= len(data) {
			continue
		}
		c := parseCookieRecord(data[cookieOffset:])
		if c != nil {
			cookies = append(cookies, *c)
		}
	}

	return cookies
}

func parseCookieRecord(data []byte) *Cookie {
	if len(data) < 48 {
		return nil
	}

	// size at 0 (LE uint32)
	flags := binary.LittleEndian.Uint32(data[4:8])
	// offsets 8-15: padding
	urlOffset := binary.LittleEndian.Uint32(data[16:20])
	nameOffset := binary.LittleEndian.Uint32(data[20:24])
	pathOffset := binary.LittleEndian.Uint32(data[24:28])
	valueOffset := binary.LittleEndian.Uint32(data[28:32])
	// offsets 32-39: comment
	expiryBits := binary.LittleEndian.Uint64(data[40:48])
	expiry := math.Float64frombits(expiryBits)

	domain := readCString(data, urlOffset)
	name := readCString(data, nameOffset)
	path := readCString(data, pathOffset)
	value := readCString(data, valueOffset)

	if name == "" {
		return nil
	}

	var expires time.Time
	if expiry != 0 {
		expires = macEpoch.Add(time.Duration(expiry * float64(time.Second)))
	}

	return &Cookie{
		Name:     name,
		Value:    value,
		Domain:   domain,
		Path:     path,
		Expires:  expires,
		Secure:   flags&0x1 != 0,
		HTTPOnly: flags&0x4 != 0,
	}
}

func readCString(data []byte, offset uint32) string {
	if int(offset) >= len(data) {
		return ""
	}
	end := offset
	for int(end) < len(data) && data[end] != 0 {
		end++
	}
	return string(data[offset:end])
}

// ExtractCookies returns cookies for the given domains as a name->value map.
// Domain matching is suffix-based: ".example.com" matches "www.example.com".
func ExtractCookies(domains []string, path string) (map[string]string, error) {
	all, err := ParseBinaryCookies(path)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, cookie := range all {
		cookieDomain := strings.TrimPrefix(cookie.Domain, ".")
		for _, domain := range domains {
			target := strings.TrimPrefix(domain, ".")
			if cookieDomain == target ||
				strings.HasSuffix(cookieDomain, "."+target) ||
				strings.HasSuffix(target, "."+cookieDomain) {
				result[cookie.Name] = cookie.Value
				break
			}
		}
	}

	if len(result) == 0 {
		log.Printf("wraith/cookies: no cookies found for domains %v", domains)
	} else {
		log.Printf("wraith/cookies: extracted %d cookies for %v", len(result), domains)
	}

	return result, nil
}

// CookieHeader formats cookies as a "Cookie:" header value.
func CookieHeader(cookies map[string]string) string {
	parts := make([]string, 0, len(cookies))
	for name, value := range cookies {
		parts = append(parts, name+"="+value)
	}
	return strings.Join(parts, "; ")
}
