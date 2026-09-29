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
	},
	"$randomLatitude":  func(s *MemStore) string { return fmt.Sprintf("%.6f", float64(s.random.Intn(180000001))/1e6-90) },
	"$randomLongitude": func(s *MemStore) string { return fmt.Sprintf("%.6f", float64(s.random.Intn(360000001))/1e6-180) },
	"$randomSemver": func(s *MemStore) string {
		return fmt.Sprintf("%d.%d.%d", s.random.Intn(10), s.random.Intn(20), s.random.Intn(50))
	},
}

func init() {
	addDynamicChoices()
	addDynamicGenerators()
}

func addDynamicChoices() {
	choices := map[string][]string{
		"$randomNamePrefix":            {"Dr.", "Mr.", "Ms.", "Prof."},
		"$randomNameSuffix":            {"Jr.", "Sr.", "II", "III"},
		"$randomJobArea":               {"Engineering", "Finance", "Marketing", "Operations"},
		"$randomJobDescriptor":         {"Lead", "Principal", "Senior", "Dynamic"},
		"$randomJobTitle":              {"Engineer", "Analyst", "Manager", "Architect"},
		"$randomJobType":               {"Designer", "Developer", "Director", "Strategist"},
		"$randomStreetName":            {"Main Street", "Market Street", "Oak Avenue", "Sunset Road"},
		"$randomCompanySuffix":         {"Inc", "LLC", "Group", "PLC"},
		"$randomCurrencyCode":          {"AUD", "CAD", "EUR", "INR", "JPY", "USD"},
		"$randomCurrencyName":          {"Dollar", "Euro", "Rupee", "Yen"},
		"$randomCurrencySymbol":        {"$", "€", "₹", "¥"},
		"$randomTransactionType":       {"deposit", "invoice", "payment", "withdrawal"},
		"$randomBankAccountName":       {"Checking", "Investment", "Savings"},
		"$randomBankAccountBic":        {"AAAABBCC", "BOFAUS3N", "DEUTDEFF"},
		"$randomProductAdjective":      {"Ergonomic", "Fantastic", "Practical", "Sleek"},
		"$randomProductMaterial":       {"Cotton", "Granite", "Metal", "Wooden"},
		"$randomProduct":               {"Chair", "Keyboard", "Shirt", "Table"},
		"$randomDepartment":            {"Books", "Electronics", "Garden", "Sports"},
		"$randomDatabaseColumn":        {"created_at", "id", "name", "status"},
		"$randomDatabaseTable":         {"accounts", "orders", "products", "users"},
		"$randomDatabaseType":          {"bigint", "boolean", "text", "varchar"},
		"$randomDatabaseCollation":     {"en_US.UTF-8", "utf8_general_ci", "utf8mb4_unicode_ci"},
		"$randomDatabaseEngine":        {"InnoDB", "MyISAM", "PostgreSQL"},
		"$randomFileType":              {"application", "image", "text", "video"},
		"$randomFileExt":               {"json", "png", "txt", "xml"},
		"$randomCommonFileType":        {"application/json", "image/png", "text/plain"},
		"$randomCommonFileExt":         {"json", "png", "txt"},
		"$randomMimeType":              {"application/json", "image/png", "text/plain"},
		"$randomDomainSuffix":          {"com", "example", "net", "org"},
		"$randomLocale":                {"de", "en", "es", "fr", "ja"},
		"$randomAbbreviation":          {"CSS", "HTTP", "JSON", "SQL"},
		"$randomNoun":                  {"application", "protocol", "request", "system"},
		"$randomVerb":                  {"build", "parse", "send", "write"},
		"$randomIngverb":               {"building", "parsing", "sending", "writing"},
		"$randomAdjective":             {"fast", "portable", "reliable", "secure"},
		"$randomCatchPhraseAdjective":  {"Adaptive", "Distributed", "Robust", "Virtual"},
		"$randomCatchPhraseDescriptor": {"foreground", "local", "open-source", "real-time"},
		"$randomCatchPhraseNoun":       {"architecture", "interface", "protocol", "system"},
		"$randomBsAdjective":           {"cross-platform", "distributed", "frictionless"},
		"$randomBsBuzz":                {"aggregate", "enable", "optimize"},
		"$randomBsNoun":                {"architectures", "interfaces", "platforms"},
	}
	for name, values := range choices {
		dynamicVariables[name] = choiceDynamic(values...)
	}
}

func addDynamicGenerators() {
	dynamicVariables["$randomNumber"] = func(s *MemStore) string { return strconv.Itoa(s.random.Intn(100000)) }
	dynamicVariables["$randomFloat"] = func(s *MemStore) string {
		return fmt.Sprintf("%.6f", float64(s.random.Intn(1000000))/1000000)
	}
	dynamicVariables["$randomInternetColor"] = dynamicVariables["$randomColor"]
	dynamicVariables["$randomMACAddress"] = func(s *MemStore) string {
		parts := make([]string, 6)
		for i := range parts {
			parts[i] = fmt.Sprintf("%02x", s.random.Intn(256))
		}
		return strings.Join(parts, ":")
	}
	dynamicVariables["$randomDomainWord"] = randomWord
	dynamicVariables["$randomDomainName"] = func(s *MemStore) string { return randomWord(s) + ".example" }
	dynamicVariables["$randomExampleEmail"] = func(s *MemStore) string {
		return randomWord(s) + strconv.Itoa(s.random.Intn(1000)) + "@example.com"
	}
	dynamicVariables["$randomFileName"] = func(s *MemStore) string { return randomWord(s) + ".txt" }
	dynamicVariables["$randomCommonFileName"] = dynamicVariables["$randomFileName"]
	dynamicVariables["$randomFilePath"] = func(s *MemStore) string { return "/tmp/" + randomWord(s) + ".txt" }
	dynamicVariables["$randomDirectoryPath"] = func(s *MemStore) string { return "/tmp/" + randomWord(s) }
	dynamicVariables["$randomPhoneNumberExt"] = func(s *MemStore) string { return randomFrom(s, digits, 4) }
	dynamicVariables["$randomBankAccountIban"] = func(s *MemStore) string {
		return "GB" + randomFrom(s, digits, 2) + randomFrom(s, alphaNumeric, 18)
	}
	dynamicVariables["$randomBitcoin"] = func(s *MemStore) string { return "1" + randomFrom(s, alphaNumeric, 33) }
	dynamicVariables["$randomProductName"] = func(s *MemStore) string {
		return capitalize(randomWord(s)) + " " + capitalize(randomWord(s))
	}
	dynamicVariables["$randomCatchPhrase"] = func(s *MemStore) string { return capitalize(randomWords(s, 3)) }
	dynamicVariables["$randomBs"] = func(s *MemStore) string { return randomWords(s, 3) }
	dynamicVariables["$randomPhrase"] = func(s *MemStore) string { return capitalize(randomWords(s, 6)) + "." }
	dynamicVariables["$randomLoremSentences"] = func(s *MemStore) string {
		return capitalize(randomWords(s, 8)) + ". " + capitalize(randomWords(s, 8)) + "."
	}
	dynamicVariables["$randomLoremParagraphs"] = func(s *MemStore) string {
		return capitalize(randomWords(s, 24)) + ".\n\n" + capitalize(randomWords(s, 24)) + "."
	}
	dynamicVariables["$randomLoremText"] = func(s *MemStore) string { return randomWords(s, 40) }
	dynamicVariables["$randomLoremSlug"] = func(s *MemStore) string {
		return strings.ReplaceAll(randomWords(s, 4), " ", "-")
	}
	dynamicVariables["$randomLoremLines"] = func(s *MemStore) string {
		return capitalize(randomWords(s, 8)) + ".\n" + capitalize(randomWords(s, 8)) + "."
	}
	for _, name := range []string{
		"$randomAvatarImage", "$randomImageUrl", "$randomAbstractImage", "$randomAnimalsImage",
		"$randomBusinessImage", "$randomCatsImage", "$randomCityImage", "$randomFoodImage",
		"$randomNightlifeImage", "$randomFashionImage", "$randomPeopleImage", "$randomNatureImage",
		"$randomSportsImage", "$randomTechnicsImage", "$randomTransportImage",
	} {
		dynamicVariables[name] = func(s *MemStore) string {
			return "https://images.example/" + randomWord(s) + ".png"
		}
	}
	dynamicVariables["$randomImageDataUri"] = func(s *MemStore) string {
		return "data:image/svg+xml;base64,PHN2Zy8+"
	}
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
