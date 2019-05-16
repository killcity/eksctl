//+build ignore

package main

import (
	"io/ioutil"
	"log"
	"strconv"
	"strings"

	"net/http"

	"fmt"
	"os"
)

const maxPodsPerNodeTypeSourceText = "https://raw.github.com/awslabs/amazon-eks-ami/master/files/eni-max-pods.txt"

// This file is used to generate a mapping between instance types and the maximum amount
// of pods that they support. This is a limitation imposed by the ENIs.
func main() {
	maxPodsMap, keys := generateMap()
	renderTextMap(maxPodsMap, keys)
}

func generateMap() (map[string]int, []string) {

	resp, err := http.Get(maxPodsPerNodeTypeSourceText)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err.Error())
	}

	dict := make(map[string]int)
	var keys []string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, " ")
		if len(parts) != 2 {
			continue
		}
		instanceType := parts[0]
		maxPods, err := strconv.Atoi(parts[1])
		if err != nil {
			log.Fatal(err.Error())
		}
		dict[instanceType] = maxPods
		keys = append(keys, instanceType)
	}

	return dict, keys
}

// renderTextMap exports the map to a txt file of <key><space><value>\n
// preserving the original order of the keys specified by "keys"
func renderTextMap(maxPodsMap map[string]int, keys []string) {
	f, err := os.Create("assets/max_pods_map.txt")
	if err != nil {
		log.Fatal(err.Error())
	}
	defer f.Close()

	for _, k := range keys {
		v := maxPodsMap[k]
		f.WriteString(fmt.Sprintf("%s %d\n", k, v))
	}
	f.Sync()
}
