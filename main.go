package main

import (
	"fmt"

	"github.com/mnightingale/go-rapidyenc/encoder"
)

func main() {
	src := make([]byte, 128+96)

	encoder := encoder.NewEncoder()

	dst, _ := encoder.Encode(src)

	// result := decoder.Decode(src)

	fmt.Println("Encoded:", dst)

	// fmt.Println("Result length:", result)
	// fmt.Println("Decoded:", src)
	// fmt.Println("Decoded:", encoder.MaxLength(len(src), 128))
}
