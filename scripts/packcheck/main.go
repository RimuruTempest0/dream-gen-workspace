// Command packcheck fails a release build when the shipping ZIP could carry the
// developer's own configuration (see internal/packcheck).
package main

import (
	"flag"
	"fmt"
	"os"

	"imageworkbench/internal/packcheck"
)

func main() {
	archive := flag.String("zip", "", "shipping ZIP to check")
	data := flag.String("data", "", "local data directory whose connections must not be shipped (optional)")
	customer := flag.Bool("customer", false, "customer package: customer.dat is allowed at the root")
	flag.Parse()
	findings, e := packcheck.Check(*archive, packcheck.Options{DataDir: *data, CustomerFile: *customer})
	if e != nil {
		fmt.Fprintln(os.Stderr, "packcheck:", e)
		os.Exit(2)
	}
	for _, f := range findings {
		fmt.Fprintln(os.Stderr, "packcheck:", f)
	}
	if len(findings) > 0 {
		os.Exit(1)
	}
	fmt.Println("packcheck: shipping ZIP contains no local data or connection details")
}
