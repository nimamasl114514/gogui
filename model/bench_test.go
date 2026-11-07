package model_test

import (
	"testing"

	"github.com/nimamasl114514/gogui/model"
)

func BenchmarkDispatchClick(b *testing.B) {
	app := model.New("b", model.Size{W: 10, H: 10},
		model.VStack(
			model.Button("x").WithID("btn").OnClick(func() {}),
			model.Label("0").WithID("lbl"),
		))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = app.Dispatch(model.Event{WidgetID: "btn", Type: "click"})
	}
}

func BenchmarkDispatchVarSet(b *testing.B) {
	v := model.NewVar(0)
	app := model.New("b", model.Size{W: 10, H: 10},
		model.VStack(model.Label("").WithID("lbl").BindTextInt(v)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.Set(i)
	}
	_ = app
}
