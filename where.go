/* Copyright © 2021 aabacchus
 * Apache License, Version 2.0
 * See LICENSE file for copyright and license details.
 *
 * Copyright (c) 2015 Matt Baer
 * MIT license
 */

// this program is a rewrite of [https://github.com/thebaer]'s tildes/where
// (MIT licence)
// which plots a map of the locations of ctrl-c.club users.
// However, that project has not been updated for several years, and it has a
// small issue with showing many IPs at the location (0,0).
// Therefore, as an excercise in Go and for the benefit of the ctrl-c.club community,
// I'm trying to make what is basically the same thing.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s\t[-h] [-k]\n", os.Args[0])
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nwhere finds users who have opted in by creating a \".here\" file in their home directory,\nfinds their approximate location from their IP address, and creates a map of the locations of those users.\n")
}

func main() {
	apiKey := flag.String("k", "", "API key for ipstack")
	flag.Usage = usage
	flag.Parse()

	ips, err := getTestIps("whoips")
	if err != nil {
		fmt.Printf("error reading sample who --ips file: %s\n", err.Error)
		os.Exit(1)
	}
	lines := parseLines(ips)
	var results = make([]person, len(lines))
	for i, line := range lines {
		if len(line) > 5 {
			// if there's something messy with mosh, ignore it (for now)
			continue
		}
		resp, err := ipLatLng(*apiKey, line[4])
		if err != nil {
			fmt.Println(err)
			continue
		}
		results[i] = person{
			Uname: line[0],
			Lat:   resp.Lat,
			Lng:   resp.Lng,
		}

	}
	fmt.Println(results)

}

type person struct {
	Uname    string
	Lat, Lng float64
}

func parseLines(ips []byte) [][]string {
	var word string
	var words [][]string
	var line int = 0
	words = append(words, []string{})
	for _, ip := range ips {
		if ip == ' ' {
			if word == "" {
				continue
			}
			words[line] = append(words[line], word)
			word = ""
			continue
		}
		if ip == '\n' {
			words[line] = append(words[line], word)
			words = append(words, []string{})
			line++
			word = ""
			continue
		}
		word = word + string(ip)
	}
	words = words[:len(words)-1]
	return words
}

func ipLatLng(apikey string, ips ...string) (IPResponse, error) {
	query := fmt.Sprintf("http://api.ipstack.com/%s?access_key=%s", strings.Join(ips, ","), apikey)
	resp, err := http.Get(query)
	if err != nil {
		return IPResponse{}, err
	}
	defer resp.Body.Close()
	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return IPResponse{}, err
	}
	places := IPResponse{}

	if err := json.Unmarshal(bytes, &places); err != nil {
		return IPResponse{}, err
	}
	return places, nil
}

// IPResponse is a simple struct wrapping relevant responses from the ipstack API
type IPResponse struct {
	ip  string  `json:"ip"`
	Lat float64 `json:"latitude"`
	Lng float64 `json:"longitude"`
}

func getTestIps(fname string) ([]byte, error) {
	f, err := os.Open(fname)
	defer f.Close()
	if err != nil {
		return []byte{}, err
	}
	return ioutil.ReadAll(f)
}
