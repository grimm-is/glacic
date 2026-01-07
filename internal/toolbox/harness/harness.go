package harness

import "fmt"

func Run(args []string) error {
	fmt.Println("Harness (prove) running with args:", args)
	return nil
}
