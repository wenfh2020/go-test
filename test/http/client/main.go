package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
)

func main() {
	url := "http://127.0.0.1:1210/hello"

	var jsonStr = []byte(`{"title":"Buy cheese and bread for breakfast."}`)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonStr))

	/* send request. */
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		panic(err)
	}

	defer res.Body.Close()

	/* response. */
	fmt.Println("response Status:", res.Status)
	fmt.Println("response Headers:", res.Header)
	bytes, _ := ioutil.ReadAll(res.Body)
	fmt.Println("response Body:", string(bytes))
}
