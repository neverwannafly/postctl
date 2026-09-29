// Package tuitest builds synthetic workspaces for TUI tests, benchmarks and
// the committed demo fixture. Everything here is invented data.
package tuitest

import (
	"context"
	"fmt"

	"github.com/shubham/postctl/internal/model"
	"github.com/shubham/postctl/internal/store"
)

// Demo returns a small workspace that exercises every row type: nested
// folders, every common method, several environments (one production, one
// with secrets).
func Demo() *model.Workspace {
	pay := model.NewCollection("Payments")
	charges := model.NewFolder("Charges")
	charges.Items = []model.Item{
		req("List charges", "GET", "{{baseUrl}}/v1/charges?limit=10", "Newest first."),
		withBody(req("Create charge", "POST", "{{baseUrl}}/v1/charges", "Charge a card.\n\nSecond paragraph."), `{"amount": {{amount}}, "currency": "usd"}`),
		req("Refund charge", "DELETE", "{{baseUrl}}/v1/charges/{{chargeId}}", ""),
		req("Update charge", "PATCH", "{{baseUrl}}/v1/charges/{{chargeId}}", ""),
	}
	customers := model.NewFolder("Customers")
	customers.Items = []model.Item{
		req("Create customer", "POST", "{{baseUrl}}/v1/customers", ""),
		req("Get customer", "GET", "{{baseUrl}}/v1/customers/{{customerId}}", ""),
		req("Replace customer", "PUT", "{{baseUrl}}/v1/customers/{{customerId}}", ""),
	}
	pay.Items = []model.Item{charges, customers, req("Health", "GET", "{{baseUrl}}/health", "")}

	admin := model.NewCollection("Internal admin")
	admin.Items = []model.Item{
		req("List users", "GET", "{{baseUrl}}/admin/users", ""),
		req("Ban user", "POST", "{{baseUrl}}/admin/users/{{userId}}/ban", ""),
		req("Preflight", "OPTIONS", "{{baseUrl}}/admin/users", ""),
	}

	prod := &model.Environment{ID: model.NewID(), Name: "prod", Production: true, Variables: []model.Variable{
		{Key: "baseUrl", Value: "https://api.example.com"},
		{Key: "apiKey", Type: model.VarSecret},
		{Key: "webhookSecret", Type: model.VarSecret},
	}}
	staging := &model.Environment{ID: model.NewID(), Name: "staging", Variables: []model.Variable{
		{Key: "baseUrl", Value: "https://staging.example.com"},
		{Key: "apiKey", Type: model.VarSecret},
		{Key: "amount", Value: "1200"},
		{Key: "currency", Value: "usd", Disabled: true},
	}}
	dev := &model.Environment{ID: model.NewID(), Name: "dev", Variables: []model.Variable{{Key: "baseUrl", Value: "https://dev.example.com"}}}
	local := &model.Environment{ID: model.NewID(), Name: "local"}

	return &model.Workspace{
		ID:           model.NewID(),
		Name:         "payments-api",
		Collections:  []*model.Collection{pay, admin},
		Environments: []*model.Environment{dev, local, prod, staging},
	}
}

// Large returns a workspace with n requests spread over collections and
// folders, for performance tests.
func Large(n int) *model.Workspace {
	ws := &model.Workspace{ID: model.NewID(), Name: "large"}
	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	made := 0
	for ci := 0; made < n; ci++ {
		c := model.NewCollection(fmt.Sprintf("Service %02d", ci))
		for fi := 0; fi < 10 && made < n; fi++ {
			f := model.NewFolder(fmt.Sprintf("Resource %02d", fi))
			for ri := 0; ri < 10 && made < n; ri++ {
				f.Items = append(f.Items, req(fmt.Sprintf("Operation %d.%d.%d", ci, fi, ri), methods[made%len(methods)],
					fmt.Sprintf("{{baseUrl}}/v1/res%d/%d", fi, ri), ""))
				made++
			}
			c.Items = append(c.Items, f)
		}
		ws.Collections = append(ws.Collections, c)
	}
	ws.Environments = []*model.Environment{{ID: model.NewID(), Name: "dev", Variables: []model.Variable{{Key: "baseUrl", Value: "https://dev.example.com"}}}}
	return ws
}

func req(name, method, url, desc string) *model.Request {
	r := model.NewRequest(name, method, url)
	r.Description = desc
	return r
}

func withBody(r *model.Request, raw string) *model.Request {
	r.Body = model.Body{Mode: model.BodyRaw, Raw: raw, Language: "json"}
	return r
}

// Write initializes a workspace at dir and saves ws into it.
func Write(dir string, ws *model.Workspace) (*store.Store, error) {
	s, err := store.Init(dir, ws.Name)
	if err != nil {
		return nil, err
	}
	return s, s.Save(context.Background(), ws)
}
