package alertchecker

import (
	"html/template"
	"log"
	"net/http"
	"time"
)

const statusTextTemplate = `
<!DOCTYPE html>
<title>PromMSD Status</title>
<style>
	body { font-family: -apple-system,system-ui,BlinkMacSystemFont,"Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif; }
	table { width: 100%; border-collapse: collapse; }
	th { font-weight: bold; }
	th, td { border: 1px solid #aaa; padding: 5px; }
	tr.good { background-color: #cfc; }
	tr.alert { background-color: #fcc; }
	button.delete { background-color: #fbb; }
</style>

<script>
  async function del(button) {
		try {
			let key = button.dataset.key;
			let r = await fetch("/modify?key=" + encodeURIComponent(key), {
				method: "DELETE"
			});
			if (r.status != 200) {
				let text = await r.text();
				alert(r.status + ": " + text);
			} else {
				window.location.reload();
			}
		} catch(e) {
			alert(e);
		}
	}
</script>

<p>
	A Prometheus monitoring safety device. See <a
	href="http://github.com/G-Research/prommsd">docs on GitHub</a>.
</p>

<p>
	Monitoring {{ len .Monitored }} instances.

{{ if len .Monitored }}
	<table>
		<tr>
			<th>Key</th>
			<th>Graph</th>
			<th>Status</th>
			<th></th>
		</tr>
		{{ range $key, $value := .Monitored }}
		<tr class="{{ if after $.Time .ActivateAt }}alert{{ else }}good{{ end }}">
			<td>{{ $key }}</td>
			<td><a href="{{ .LastAlert.GeneratorURL }}">Graph</a></td>
			<td>
				{{ if after $.Time .ActivateAt }}
					Activated {{ humanise $.Time .ActivateAt }} ago
				{{ else }}
					Activate in {{ humanise $.Time .ActivateAt }}
				{{ if after .ActivatedAt $.Zero }}
					<br>
					Alert last activated {{ humanise $.Time .ActivatedAt }} ago
				{{ end }}
				{{ end }}
				{{ if after .ResolvedAt $.Zero }}
					<br>
					Last resolved {{ humanise $.Time .ResolvedAt }} ago
				{{ end }}
				{{ if after .LastSent $.Zero }}
					<br>
					Last sent: {{ humanise $.Time .LastSent }} ago (includes resolved alerts)
				{{ end }}
				{{ if .LastError}}
					<br>
					Last error: {{ .LastError }}
				{{ end }}
			</td>
			<td>
			  <button class="delete" data-key="{{$key}}" onclick="del(this)">Delete</button>
			</td>
		</tr>
		{{ end }}
	</table>
{{ end }}

<p>
	Debug info:
	<ul>
		<li><a href="/debug/requests">requests</a>
		<li><a href="/debug/events">events</a>
	</ul>
</p>
`

var funcMap = template.FuncMap{
	"humanise": humanise,
	"after":    after,
}

var statusTemplate = template.Must(template.New("status").Funcs(funcMap).Parse(statusTextTemplate))

func (ac *AlertChecker) status(w http.ResponseWriter, req *http.Request) {
	ac.RLock()
	defer ac.RUnlock()

	err := statusTemplate.Execute(w, map[string]interface{}{
		"Monitored": ac.monitored,
		"Time":      time.Now(),
		"Zero":      time.Unix(0, 0),
	})

	if err != nil {
		log.Printf("Error serving status: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Responds to /modify?key=... requests
func (ac *AlertChecker) modify(w http.ResponseWriter, req *http.Request) {
	ac.Lock()
	defer ac.Unlock()

	key := req.FormValue("key")
	if _, ok := ac.monitored[key]; !ok {
		http.Error(w, "Key does not exist", http.StatusBadRequest)
		return
	}

	if req.Method != "DELETE" {
		http.Error(w, "Only DELETE currently supported", http.StatusBadRequest)
		return
	}

	delete(ac.monitored, key)
	w.Write([]byte("ok"))
}

func after(a, b time.Time) bool {
	return a.After(b)
}

func humanise(now, t time.Time) string {
	var diff time.Duration
	if now.After(t) {
		diff = now.Sub(t)
	} else {
		diff = t.Sub(now)
	}
	return diff.Round(time.Second).String()
}
