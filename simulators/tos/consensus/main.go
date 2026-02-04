package main

import "github.com/tos-network/lab/labsim"

func main() {
	sim := labsim.New()

	suite := labsim.Suite{
		Name:        "tos/consensus",
		Description: "Consensus conformance suite (skeleton)",
	}

	suite.Add(labsim.TestSpec{
		Name:        "consensus/skeleton",
		Description: "Placeholder test case for consensus suite",
		Run: func(t *labsim.T) {
			// TODO: implement block execution + fork choice tests
		},
	})

	labsim.MustRunSuite(sim, suite)
}
