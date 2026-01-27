package cleanser

import (

	"regexp"

	"strconv"

	"strings"

	"time"
)

var (

	emailRe = regexp.MustCompile(`^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}$`)
)

func NormalizeEmail(s string) string {

	s = strings.ToLower(strings.TrimSpace(s))

	if s == "" {


		return ""

	}

	if !emailRe.MatchString(s) {


		return ""

	}

	return s
}

func NormalizePhoneUS(s string) string {

	// keep digits

	d := make([]byte, 0, len(s))

	for i := 0; i < len(s); i++ {


		if s[i] >= '0' && s[i] <= '9' {



			d = append(d, s[i])


		}

	}

	if len(d) == 11 && d[0] == '1' {


		d = d[1:]

	}

	if len(d) != 10 {


		return ""

	}

	return "+1" + string(d)
}

func NormalizeDateISO(s string) string {

	s = strings.TrimSpace(s)

	if s == "" {


		return ""

	}


	// Try known layouts

	layouts := []string{


		"2006-01-02",


		"2006/01/02",


		"01/02/2006",


		"January 2, 2006",


		"Jan 2, 2006",

	}

	for _, layout := range layouts {


		if t, err := time.Parse(layout, s); err == nil {



			return t.Format("2006-01-02")


		}

	}


	return ""
}

func NormalizeMoney(s string) string {

	s = strings.TrimSpace(s)

	if s == "" {


		return ""

	}

	// strip currency symbols/commas

	buf := make([]byte, 0, len(s))

	dotSeen := false

	for i := 0; i < len(s); i++ {


		ch := s[i]


		if ch >= '0' && ch <= '9' {



			buf = append(buf, ch)



			continue


		}


		if ch == '.' && !dotSeen {



			dotSeen = true



			buf = append(buf, ch)



			continue


		}

	}

	if len(buf) == 0 {


		return ""

	}

	f, err := strconv.ParseFloat(string(buf), 64)

	if err != nil {


		return ""

	}

	// format 2 decimals

	cents := int64(f*100 + 0.5)

	return strconv.FormatInt(cents/100, 10) + "." + twoDigits(int(cents%100))
}

func twoDigits(n int) string {

	if n < 0 {


		n = -n

	}

	if n < 10 {


		return "0" + strconv.Itoa(n)

	}

	return strconv.Itoa(n)
}
