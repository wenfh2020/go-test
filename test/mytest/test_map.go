package main

import (
	"fmt"
)

func testMap(m map[string]int) {
	m["test"] = 3
}

// TestMapLogic for map test exsample
func testMapLogic() {
	mapTest := map[string]int{"11": 1, "22": 2}
	for k, v := range mapTest {
		fmt.Println(k, v)
	}

	fmt.Println("...................")

	testMap(mapTest)
	// delete(mapTest, "11")
	for k, v := range mapTest {
		fmt.Println(k, v)
	}

	getType(mapTest)
}
