package alertmanager

import "time"

// Message is the JSON message sent by Prometheus Alertmanager, see
// https://prometheus.io/docs/alerting/configuration/#webhook_config for
// documentation on fields.
type Message struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	Status            string            `json:"status"`
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []*Alert          `json:"alerts"`
}

type Alert struct {
	Parent       *Message          `json:"-"`
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
}

func NewAlert() Alert {
	return Alert{
		Status:      "firing",
		Labels:      make(map[string]string),
		Annotations: make(map[string]string),
	}
}

// GetLabel gets a label from an alert. It will fetch labels in Alert.Label
// with fallback to Parent.CommonLabels and Parent.GroupLabels for you.
func (a *Alert) GetLabel(key string) (value string, ok bool) {
	if v, ok := a.Labels[key]; ok {
		return v, ok
	}
	if a.Parent == nil {
		return "", false
	}
	if v, ok := a.Parent.CommonLabels[key]; ok {
		return v, ok
	}
	if v, ok := a.Parent.GroupLabels[key]; ok {
		return v, ok
	}
	return "", false
}

// GetLabelDefault gets a label from an alert, returning a default if it isn't
// present.
func (a *Alert) GetLabelDefault(key, def string) string {
	if v, ok := a.GetLabel(key); ok {
		return v
	}
	return def
}

// GetLabels returns a map of all the labels on an alert.
func (a *Alert) GetLabels() map[string]string {
	l := make(map[string]string)
	if a.Parent != nil {
		for k, v := range a.Parent.GroupLabels {
			l[k] = v
		}
		for k, v := range a.Parent.CommonLabels {
			l[k] = v
		}
	}
	for k, v := range a.Labels {
		l[k] = v
	}
	return l
}

// GetAnnotation gets an annotation from an alert. It will fetch annotations in
// Alert.Annotations with fallback to Parent.CommonAnnotations for you.
func (a *Alert) GetAnnotation(key string) (value string, ok bool) {
	if v, ok := a.Annotations[key]; ok {
		return v, ok
	}
	if a.Parent == nil {
		return "", false
	}
	if v, ok := a.Parent.CommonAnnotations[key]; ok {
		return v, ok
	}
	return "", false
}

// GetAnnotationDefault gets an annotation from an alert, returning a default
// if it isn't present.
func (a *Alert) GetAnnotationDefault(key, def string) string {
	if v, ok := a.GetAnnotation(key); ok {
		return v
	}
	return def
}

// GetAnnotations returns a map of all the annotations on an alert.
func (a *Alert) GetAnnotations() map[string]string {
	l := make(map[string]string)
	if a.Parent != nil {
		for k, v := range a.Parent.CommonAnnotations {
			l[k] = v
		}
	}
	for k, v := range a.Annotations {
		l[k] = v
	}
	return l
}
