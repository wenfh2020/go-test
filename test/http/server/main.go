/* https://studygolang.com/articles/14947?utm_medium=referral */

package main

import (
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/hello", hello)
	log.Println("start http server, port: 1210.")
	log.Fatal(http.ListenAndServe(":1210", nil))
}

func hello(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("hello world!"))
}
