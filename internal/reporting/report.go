package reporting

import "time"

type UptimeReport struct {
	ServerID      string    `json:"server_id"`
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
	Checks        int       `json:"checks"`
	Successful    int       `json:"successful"`
	UptimePercent float64   `json:"uptime_percent"`
}

func Build(id string, from, to time.Time, values []bool) UptimeReport {
	ok := 0
	for _, v := range values {
		if v {
			ok++
		}
	}
	p := 0.0
	if len(values) > 0 {
		p = 100 * float64(ok) / float64(len(values))
	}
	return UptimeReport{id, from, to, len(values), ok, p}
}
