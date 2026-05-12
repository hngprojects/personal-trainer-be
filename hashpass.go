package main

import (
	"fmt"
	"log"

	"github.com/hngprojects/personal-trainer-be/internal/auth"
)

func main() {
	h, err := auth.HashPassword("StrongPass1!")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(h)
}