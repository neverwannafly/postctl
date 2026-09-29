package vars

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type dynamicFunc func(*MemStore) string

var dynamicVariables = map[string]dynamicFunc{
	"$guid":         dynamicUUID,
	"$randomUUID":   dynamicUUID,
	"$timestamp":    func(*MemStore) string { return strconv.FormatInt(time.Now().Unix(), 10) },
	"$isoTimestamp": func(*MemStore) string { return time.Now().UTC().Format(time.RFC3339Nano) },
	"$randomInt":    func(s *MemStore) string { return strconv.Itoa(s.random.Intn(1001)) },
	"$randomEmail": func(s *MemStore) string {
		return strings.ToLower(randomWord(s)) + strconv.Itoa(s.random.Intn(1000)) + "@example.com"
	},
	"$randomFirstName": choiceDynamic("Avery", "Jordan", "Morgan", "Riley", "Taylor", "Casey"),
	"$randomLastName":  choiceDynamic("Smith", "Patel", "Garcia", "Kim", "Brown", "Nguyen"),
	"$randomFullName": func(s *MemStore) string {
		return randomFirstName(s) + " " + randomLastName(s)
	},
	"$randomAlphaNumeric": func(s *MemStore) string { return randomFrom(s, alphaNumeric, 1) },
	"$randomBoolean":      func(s *MemStore) string { return strconv.FormatBool(s.random.Intn(2) == 1) },
	"$randomColor":        func(s *MemStore) string { return fmt.Sprintf("#%06x", s.random.Intn(1<<24)) },
	"$randomHexColor":     func(s *MemStore) string { return fmt.Sprintf("#%06x", s.random.Intn(1<<24)) },
	"$randomIP":           randomIP,
	"$randomIPV4":         randomIP,
	"$randomIPV6":         randomIPv6,
	"$randomUserName":     func(s *MemStore) string { return strings.ToLower(randomWord(s)) + strconv.Itoa(s.random.Intn(10000)) },
	"$randomPassword":     func(s *MemStore) string { return randomFrom(s, alphaNumeric+"!@#$%^&*", 16) },
	"$randomPhoneNumber": func(s *MemStore) string {
		return fmt.Sprintf("+1-%03d-%03d-%04d", 200+s.random.Intn(800), s.random.Intn(1000), s.random.Intn(10000))
	},
	"$randomCity":           choiceDynamic("Austin", "Berlin", "Lima", "Nairobi", "Osaka", "Toronto"),
	"$randomCountry":        choiceDynamic("Australia", "Brazil", "Canada", "India", "Japan", "Kenya"),
	"$randomCountryCode":    choiceDynamic("AU", "BR", "CA", "IN", "JP", "KE"),
	"$randomCompanyName":    func(s *MemStore) string { return capitalize(randomWord(s)) + " Labs" },
	"$randomUrl":            func(s *MemStore) string { return "https://" + randomWord(s) + ".example/" + randomWord(s) },
	"$randomURL":            func(s *MemStore) string { return "https://" + randomWord(s) + ".example/" + randomWord(s) },
	"$randomWord":           randomWord,
	"$randomWords":          func(s *MemStore) string { return randomWords(s, 3) },
	"$randomLoremWord":      randomWord,
	"$randomLoremWords":     func(s *MemStore) string { return randomWords(s, 5) },
	"$randomLoremSentence":  func(s *MemStore) string { return capitalize(randomWords(s, 8)) + "." },
	"$randomLoremParagraph": func(s *MemStore) string { return capitalize(randomWords(s, 24)) + "." },
	"$randomDateFuture":     func(s *MemStore) string { return randomDate(s, 1, 365) },
	"$randomDatePast":       func(s *MemStore) string { return randomDate(s, -365, -1) },
	"$randomDateRecent":     func(s *MemStore) string { return randomDate(s, -7, 0) },
	"$randomDateSoon":       func(s *MemStore) string { return randomDate(s, 0, 7) },
	"$randomMonth":          choiceDynamic("January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"),
	"$randomWeekday":        choiceDynamic("Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"),
	"$randomPrice":          func(s *MemStore) string { return fmt.Sprintf("%.2f", float64(s.random.Intn(100000))/100) },
	"$randomCreditCardMask": func(s *MemStore) string { return "****-****-****-" + randomFrom(s, digits, 4) },
	"$randomBankAccount":    func(s *MemStore) string { return randomFrom(s, digits, 12) },
	"$randomStreetAddress": func(s *MemStore) string {
		return strconv.Itoa(1+s.random.Intn(9999)) + " " + capitalize(randomWord(s)) + " Street"
	}, //nolint:staticcheck // Fixed ASCII words.
	"$randomLatitude":  func(s *MemStore) string { return fmt.Sprintf("%.6f", float64(s.random.Intn(180000001))/1e6-90) },
	"$randomLongitude": func(s *MemStore) string { return fmt.Sprintf("%.6f", float64(s.random.Intn(360000001))/1e6-180) },
	"$randomSemver": func(s *MemStore) string {
		return fmt.Sprintf("%d.%d.%d", s.random.Intn(10), s.random.Intn(20), s.random.Intn(50))
	},
}

const (
	alphaNumeric = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	digits       = "0123456789"
)

var loremWords = []string{
	"adipiscing", "amet", "consectetur", "dolor", "elit", "ipsum", "lorem",
	"mauris", "nunc", "porta", "semper", "sit", "tempor", "vitae",
}

func (s *MemStore) dynamic(name string) (string, bool) {
	generate, ok := dynamicVariables[name]
	if !ok {
		return "", false
	}
	return generate(s), true
}

func choiceDynamic(values ...string) dynamicFunc {
	return func(s *MemStore) string { return values[s.random.Intn(len(values))] }
}

func randomFirstName(s *MemStore) string {
	return []string{"Avery", "Jordan", "Morgan", "Riley", "Taylor", "Casey"}[s.random.Intn(6)]
}

func randomLastName(s *MemStore) string {
	return []string{"Smith", "Patel", "Garcia", "Kim", "Brown", "Nguyen"}[s.random.Intn(6)]
}

func dynamicUUID(s *MemStore) string {
	var raw [16]byte
	if _, err := s.random.Read(raw[:]); err != nil {
		for i := range raw {
			raw[i] = byte(s.random.Intn(256))
		}
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

func randomFrom(s *MemStore, alphabet string, length int) string {
	var result strings.Builder
	result.Grow(length)
	for range length {
		result.WriteByte(alphabet[s.random.Intn(len(alphabet))])
	}
	return result.String()
}

func randomWord(s *MemStore) string {
	return loremWords[s.random.Intn(len(loremWords))]
}

func randomWords(s *MemStore, count int) string {
	words := make([]string, count)
	for i := range words {
		words[i] = randomWord(s)
	}
	return strings.Join(words, " ")
}

func randomIP(s *MemStore) string {
	return fmt.Sprintf("%d.%d.%d.%d", 1+s.random.Intn(223), s.random.Intn(256), s.random.Intn(256), 1+s.random.Intn(254))
}

func randomIPv6(s *MemStore) string {
	parts := make([]string, 8)
	for i := range parts {
		parts[i] = fmt.Sprintf("%x", s.random.Intn(1<<16))
	}
	return strings.Join(parts, ":")
}

func randomDate(s *MemStore, minDays, maxDays int) string {
	offset := minDays
	if maxDays > minDays {
		offset += s.random.Intn(maxDays - minDays + 1)
	}
	return time.Now().UTC().AddDate(0, 0, offset).Format(time.RFC3339Nano)
}

func capitalize(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
