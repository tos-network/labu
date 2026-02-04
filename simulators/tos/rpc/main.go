package main

import (
	"fmt"

	"github.com/tos-network/lab/labsim"
)

func main() {
	sim := labsim.New()
	clients := labsim.ClientList()
	if len(clients) == 0 {
		panic("LAB_CLIENTS is empty")
	}

	suite := labsim.Suite{
		Name:        "tos/rpc",
		Description: "RPC conformance suite (health + basic checks)",
	}

	for _, client := range clients {
		cname := client
		suite.AddClient(labsim.ClientTestSpec{
			Name:        fmt.Sprintf("%s/health", cname),
			Description: "Health endpoint should respond",
			Client:      cname,
			Run: func(t *labsim.T, c *labsim.Client) {
				exit, _, stderr, err := c.Exec([]string{"curl", "-fsS", "http://127.0.0.1:8080/health"})
				if err != nil || exit != 0 {
					t.Failf("health check failed: %v %s", err, stderr)
				}
			},
		})
	}

	labsim.MustRunSuite(sim, suite)
}
