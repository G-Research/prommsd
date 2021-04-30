package alertchecker

import (
	"encoding/json"

	"github.com/G-Research/prommsd/pkg/alertmanager"
)

func (ac *AlertChecker) sendAlerts(ctx context.Context, alertmanagers []string, resolved bool, groupLabels map[string]string, alert []alertmanager.Alert) error {
	var lastErr error
	t := "alert"
	if resolved {
		t = "resolved"
	}
	for _, alertURL := range alertmanagers {
		u, err := url.Parse(alertURL)
		if err != nil {
			log.Printf("Unable to parse alert destination URL %q: %v", alertURL, err)
			continue
		}

		// Accept type+http:// to allow specifing the kind of service.
		// Without + (e.g. http:// or https://) default to "am" (i.e.
		// "alertmanager").
		deliverType := "am"
		extraScheme := strings.SplitN(u.Scheme, "+", 2)
		if len(extraScheme) == 2 {
			deliverType = extraScheme[0]
			u.Scheme = extraScheme[1]
		}

		switch deliverType {
		case "am":
			func() {
				client := alertmanager.NewClient(u)
				ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
				defer cancel()
				log.Printf("Sending %s to %v", t, u)
				err := client.SendAlerts(ctx, alert)
				if err != nil {
					log.Printf("Error sending %s to %v: %v", t, u, err)
					lastErr = err
				}
			}()
		case "webhook":
			if err := sendWebhook(ctx, u, resolved, groupLabels, alert); err != nil {
				log.Printf("Error sending %s to %v: %v", t, u, err)
				lastErr = err
			}
		default:
			lastErr = fmt.Errorf("Unknown alert delivery type %v (in %q)", deliverType, alertURL)
			log.Print(err)
		}
	}
	return lastErr
}

// sendWebhook sends to an alertmanager webhook compatible endpoint.
func sendWebhook(ctx context.Context, sendURL *url.URL, resolved bool, groupLabels map[string]string, alerts []alertmanager.Alert) error {
	status := "firing"
	if resolved {
		status = "resolved"
	}
	// Mostly compatible with https://prometheus.io/docs/alerting/latest/configuration/#webhook_config
	body := map[string]interface{}{
		"version":           "4",
		"status":            status,
		"receiver":          "prommsd",
		"groupLabels":       groupLabels,
		"commonLabels":      alerts[0].Labels,
		"commonAnnotations": alerts[0].Annotations,
		"alerts":            alerts,
	}
	j, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := http.Post(sendURL.String(), "application/json", bytes.NewBuffer(j))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("Response %v", resp.Status)
	}
	return nil
}
