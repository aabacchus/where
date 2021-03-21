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
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
)

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s\t[-h] [-k]\n\t\t[-mboxu -mboxa -mboxs] [-mboxp]\n", os.Args[0])
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nwhere finds users who have opted in by creating a \".here\" file in their home directory,\nfinds their approximate location from their IP address, and creates a map of the locations of those users.\n")
}

func main() {
	apiKey := flag.String("k", "", "API key for ipstack")
	var mboxDetails MapboxDetails
	flag.StringVar(&mboxDetails.Uname, "mboxu", "", "mapbox.com username")
	flag.StringVar(&mboxDetails.Apikey, "mboxa", "", "mapbox.com API key")
	flag.StringVar(&mboxDetails.Style, "mboxs", "", "mapbox map style")
	flag.StringVar(&mboxDetails.Padding, "mboxp", "5", "mapbox map padding (a percentage without the %)")
	flag.Usage = usage
	flag.Parse()

	ips, err := getTestIps("whoips")
	if err != nil {
		fmt.Printf("error reading sample who --ips file: %s\n", err.Error())
		os.Exit(1)
	}
	lines := parseLines(ips)
	responseChan := make(chan MarkResponse)
	var results = make([]Marker, len(lines))
	for _, line := range lines {
		if len(line) > 5 {
			// if there's something messy eg. with mosh, ignore it (for now)
			//line[4] = ""
		}
		go ipLatLng(*apiKey, line[0], line[4], responseChan)
	}
	var resp MarkResponse
	for i := range lines {
		resp = <-responseChan
		if resp.Err != nil {
			fmt.Printf("error getting ip location for %s: %s\n", resp.Mark.Name, resp.Err)
			continue
		}
		fmt.Println(resp.Mark)
		results[i] = resp.Mark
	}
	// check if there's a file of results already
	cacheFname := "ips.json"
	if _, err := os.Stat(cacheFname); !os.IsNotExist(err) {
		// file exists, so read from file
		f, err := os.Open(cacheFname)
		if err != nil {
			fmt.Printf("error opening ips cache: %s\n", err)
			os.Exit(1)
		}
		bytes, err := ioutil.ReadAll(f)
		if err != nil {
			fmt.Printf("error reading ips cache: %s\n", err)
			os.Exit(1)
		}
		f.Close()
		var cache []Marker
		err = json.Unmarshal(bytes, &cache)
		if err != nil {
			fmt.Printf("error unmarshalling ips cache: %s\n", err)
			os.Exit(1)
		}
		// if we have newer data for a user in results, use that
		// so remove duplicates from the cache
		for _, res := range results {
			for i, c := range cache {
				if res.Name == c.Name {
					// remove the duplicate
					cache = append(cache[:i], cache[i+1:]...)
				}
			}
		}
		// now add the results to the cache (newer results at the bottom)
		results = append(cache, results...)
	}
	err = MarkersSaveJson(results, "ips.json")
	if err != nil {
		fmt.Printf("error saving as json: %s\n", err)
		os.Exit(1)
	}

	// make a map of the markers and save it as a png
	imageFile := "map.png"
	err = MapboxStatic(results, imageFile, mboxDetails)
	if err != nil {
		fmt.Printf("error creating static map: %s\n", err)
		os.Exit(1)
	}
	fmt.Printf("saved static map to %s\n", imageFile)
}

func MarkersSaveJson(m []Marker, fname string) error {
	f, err := os.Create(fname)
	if err != nil {
		return err
	}
	defer f.Close()

	bytes, err := json.MarshalIndent(m, "", "\t")
	if err != nil {
		return err
	}
	_, err = f.Write(bytes)
	return err
}

type Marker struct {
	Name     string
	Lat, Lng float64
}

type MarkResponse struct {
	Mark Marker
	Err  error
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
			// remove duplicates by username
			isDuplicate := false
			// no need to check if we're on the first line
			if line != 0 {
				for _, prevline := range words[:line] {
					if words[line][0] == prevline[0] {
						isDuplicate = true
					}
				}
			}
			if isDuplicate {
				words[line] = []string{}
				word = ""
			} else {
				words[line] = append(words[line], word)
				words = append(words, []string{})
				line++
				word = ""
			}
			continue
		}
		word = word + string(ip)
	}
	words = words[:len(words)-1]
	return words
}

func ipLatLng(apikey, name, ip string, ch chan MarkResponse) {
	if ip == "" {
		ch <- MarkResponse{Marker{Name: name}, errors.New("no IP provided")}
		return
	}
	query := fmt.Sprintf("http://api.ipstack.com/%s?access_key=%s", ip, apikey)
	resp, err := http.Get(query)
	if err != nil {
		ch <- MarkResponse{Marker{Name: name}, err}
		return
	}
	defer resp.Body.Close()
	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		ch <- MarkResponse{Marker{Name: name}, err}
		return
	}
	place := struct {
		Lat float64 `json:"latitude"`
		Lng float64 `json:"longitude"`
	}{}

	if err := json.Unmarshal(bytes, &place); err != nil {
		ch <- MarkResponse{Marker{Name: name}, err}
		return
	}

	ch <- MarkResponse{Marker{
		Name: name,
		Lat:  place.Lat,
		Lng:  place.Lng,
	}, nil}
	return
}

func getTestIps(fname string) ([]byte, error) {
	f, err := os.Open(fname)
	defer f.Close()
	if err != nil {
		return []byte{}, err
	}
	return ioutil.ReadAll(f)
}
