package alertmanager

import (
	"reflect"
	"testing"
)

func TestLabels(t *testing.T) {
	a := NewAlert()
	_, ok := a.GetLabel("a")
	if ok {
		t.Errorf("got ok, wanted not ok")
	}
	a.Labels["a"] = "value1"
	label, ok := a.GetLabel("a")
	if !ok || label != "value1" {
		t.Errorf("got %q, %v, wanted value1, true", label, ok)
	}

	a.Parent = &Message{}
	a.Parent.CommonLabels = map[string]string{
		"b": "value2",
	}
	label, ok = a.GetLabel("b")
	if !ok || label != "value2" {
		t.Errorf("got %q, %v, wanted value2, true", label, ok)
	}

	_, ok = a.GetLabel("c")
	if ok {
		t.Errorf("got ok, wanted not ok")
	}
	a.Parent.GroupLabels = map[string]string{
		"c": "value3",
	}
	label, ok = a.GetLabel("c")
	if !ok || label != "value3" {
		t.Errorf("got %q, %v, wanted value3, true", label, ok)
	}

	label = a.GetLabelDefault("d", "value4")
	if label != "value4" {
		t.Errorf("got %v, wanted value4", label)
	}

	got := a.GetLabels()
	want := map[string]string{
		"a": "value1",
		"b": "value2",
		"c": "value3",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAnnotations(t *testing.T) {
	a := NewAlert()
	_, ok := a.GetAnnotation("a")
	if ok {
		t.Errorf("got ok, wanted not ok")
	}
	a.Annotations["a"] = "value1"
	annotation, ok := a.GetAnnotation("a")
	if !ok || annotation != "value1" {
		t.Errorf("got %q, %v, wanted value1, true", annotation, ok)
	}

	a.Parent = &Message{}
	a.Parent.CommonAnnotations = map[string]string{
		"b": "value2",
	}
	annotation, ok = a.GetAnnotation("b")
	if !ok || annotation != "value2" {
		t.Errorf("got %q, %v, wanted value2, true", annotation, ok)
	}

	annotation = a.GetAnnotationDefault("c", "value3")
	if annotation != "value3" {
		t.Errorf("got %v, wanted value3", annotation)
	}

	got := a.GetAnnotations()
	want := map[string]string{
		"a": "value1",
		"b": "value2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
