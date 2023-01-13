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
	"os/exec"
	"strings"
)

var verbose *bool

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s\t[-h] [-p] [-v]\n\t\t[-c | -k -mboxu -mboxa -mboxs [-mboxp]]\n", os.Args[0])
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nwhere finds users who have opted in by creating a \".here\" file in their home directory,\nfinds their approximate location from their IP address, and creates a map of the locations of those users.\n")
}

// exists returns true if fname is a file that exists.
func exists(fname string) bool {
	_, err := os.Stat(fname)
	return !os.IsNotExist(err)
}

// isOptedIn checks if the user has opted in by having a file
// named ".here" or ".somewhere" in their home directory.
// (all data is anonymous so both are used in the same way)
func isOptedIn(user string) bool {
	homedir := fmt.Sprintf("/home/%s/", user)
	return exists(homedir+".here") || exists(homedir+".somewhere")
}

func log(msg ...interface{}) {
	if *verbose {
		fmt.Println(msg...)
	}
}

func main() {
	apiKey := flag.String("k", "", "API key for ipstack")
	usePretendWhoips := flag.Bool("p", false, "use a cached output of who --ips")
	useCredFile := flag.String("c", "", "read credentials from a json file (keys are command-line flags)")
	verbose = flag.Bool("v", false, "turn on verbose output")
	var mboxDetails MapboxDetails
	flag.StringVar(&mboxDetails.Uname, "mboxu", "", "mapbox.com username")
	flag.StringVar(&mboxDetails.Apikey, "mboxa", "", "mapbox.com API key")
	flag.StringVar(&mboxDetails.Style, "mboxs", "", "mapbox map style")
	flag.IntVar(&mboxDetails.Padding, "mboxp", 5, "mapbox map padding (a percentage without the %)")
	flag.Usage = usage
	flag.Parse()

	// get the who data, either from a file or the command itself
	var ips []byte
	var err error
	if *usePretendWhoips {
		ips, err = read("whoips")
		if err != nil {
			fmt.Printf("error reading sample who --ips file: %s\n", err.Error())
			os.Exit(1)
		}
	} else {
		ips, err = exec.Command("who", "--ips").Output()
		if err != nil {
			fmt.Println("error running `who --ips`: " + err.Error())
		}
	}

	// if necessary, get the credentials from a file
	// (overrides credentials specified with flags)
	if *useCredFile != "" {
		bytes, err := read(*useCredFile)
		if err != nil {
			fmt.Printf("error reading cred file: %s\n", err)
			os.Exit(1)
		}
		var creds struct {
			K     string
			Mboxa string
			Mboxp int
			Mboxs string
			Mboxu string
		}
		err = json.Unmarshal(bytes, &creds)
		if err != nil {
			fmt.Printf("error unmarshalling creds: %s\n", err)
			os.Exit(1)
		}
		// set global variables to our credentials
		*apiKey = creds.K
		mboxDetails = MapboxDetails{
			Apikey:  creds.Mboxa,
			Padding: creds.Mboxp,
			Style:   creds.Mboxs,
			Uname:   creds.Mboxu,
		}
	}

	// extract data from the who output
	rawlines := parseLines(ips)
	log("raw who data: ", rawlines)
	lines := make([][]string, 0)

	// only keep users who have opted in
	for _, line := range rawlines {
		name := line[0]
		if isOptedIn(name) {
			lines = append(lines, line)
		}
	}

	responseChan := make(chan MarkResponse)
	var results = make([]Marker, len(lines))
	for _, line := range lines {
		ip := line[4]
		if ip[0] == '(' {
			if strings.Contains(ip, "mosh") || strings.Contains(ip, "tmux") {
				ip = ""
			} else {
				endidx := strings.Index(ip, ":")
				if endidx == -1 {
					endidx = strings.Index(ip, ")")
					if endidx == -1 {
						endidx = len(ip)
					}
				}
				ip = ip[1:endidx]
			}
		}
		go ipLatLng(*apiKey, line[0], ip, responseChan)
	}

	var resp MarkResponse
	for i := range lines {
		resp = <-responseChan
		if resp.Err != nil {
			fmt.Printf("error getting ip location for %s: %s\n", resp.Mark.Name, resp.Err)
			continue
		}
		log("found location: ", resp.Mark)
		results[i] = resp.Mark
	}
	// check if there's a file of results already
	cacheFname := "ips.json"
	if exists(cacheFname) {
		bytes, err := read(cacheFname)
		if err != nil {
			fmt.Printf("error reading ips cache: %s\n", err)
			os.Exit(1)
		}
		log(fmt.Sprintf("found previous results file %s", cacheFname))
		var cache []Marker
		err = json.Unmarshal(bytes, &cache)
		if err != nil {
			fmt.Printf("error unmarshalling ips cache: %s\n", err)
			os.Exit(1)
		}
		// make some temporary maps in order to merge the two slices
		tmpCache := MarkersMakeMap(cache)
		tmpResults := MarkersMakeMap(results)
		var out []Marker
		// remove duplicates, using the one which isn't 0,0
		// append the good Marker to out
		// also check that the user is still opted in
		for k, val := range tmpResults {
			if cachedVal, ok := tmpCache[k]; ok && isOptedIn(k) {
				if val.Lat == 0 && val.Lng == 0 {
					out = append(out, cachedVal)
				} else {
					out = append(out, val)
				}
			} else {
				// not a duplicate
				out = append(out, val)
			}
		}
		// add the cached values which aren't in the new lot
		// also check that the user is still opted in
		for k, cVal := range tmpCache {
			if _, ok := tmpResults[k]; !ok && isOptedIn(k) {
				out = append(out, cVal)
			}
		}
		results = out
		log(results)
	}
	// save our results to a json file
	err = MarkersSaveJson(results, "ips.json")
	if err != nil {
		fmt.Printf("error saving as json: %s\n", err)
		os.Exit(1)
	}

	err = LeafletDynamic(results, "dynamic.html")
	if err != nil {
		fmt.Printf("error making leaflet dynamic map: %s\n", err)
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

func LeafletDynamic(m []Marker, fname string) error {
	f, err := os.Create(fname)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, `<!DOCTYPE html>
<html>
<!-- copyright 2023 ~phoebos <phoebos@ctrl-c.club> -->
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>dynamic map of ctrl-c club users</title>
<link rel="stylesheet" href="style.css">
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.3/dist/leaflet.css" integrity="sha256-kLaT2GOSpHechhsozzB+flnD+zUyjE2LlfWPgU04xyI=" crossorigin=""/>
<script src="https://unpkg.com/leaflet@1.9.3/dist/leaflet.js"
     integrity="sha256-WBkoXOwTeyKclOHuWtc+i2uENFpDZ9YPdf5Hf+D7ewM="
     crossorigin=""></script>
</head>
<body>
<h1>Map of Ctrl-C.Club users (dynamic version)</h1>
<p>To opt-in, add a file named <code style="font-family: monospace">.here</code> to your home directory.
The map is updated every 15 minutes.</p>
<p>To view a map without Javascript, see <a href="map.html">here</a>.</p>
<div id="map" style="height: 500px;"></div>
<p><a href="https://github.com/aabacchus/where">Repo</a></p>
<p><a href="/~bear/where.html">Inspiration</a></p>
<script>
    var map = L.map('map').setView([20,10], 1);
    L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
        maxzoom: 19,
        attribution: '&copy; <a href="http://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
    }).addTo(map);`)

	for _, k := range m {
		if k.Lat == 0 && k.Lng == 0 {
			continue
		}
		_, err = fmt.Fprintf(f, "L.marker([%f,%f]).addTo(map);\n", k.Lat, k.Lng)
		if err != nil {
			return err
		}
	}
	fmt.Fprintf(f, `</script></body></html>`)
	return err
}

func MarkersMakeMap(m []Marker) map[string]Marker {
	r := make(map[string]Marker)
	for _, k := range m {
		r[k.Name] = k
	}
	return r
}

// MarkersSaveJson saves a slice of Markers to fname in json format.
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

// Marker holds the data for a named location.
type Marker struct {
	Name     string
	Lat, Lng float64
}

// MarkResponse is used as a channel to get the results from ipLatLng.
// Making a struct and sending the results on a channel isn't a great method
// (I intend to make this better)
type MarkResponse struct {
	Mark Marker
	Err  error
}

// parseLines converts a slice of bytes into a slice of slices of strings,
// where the bytes are separated by whitespace and newlines.
// Lines in the input with the same first field are considered
// duplicates, and only one version is kept.
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

// ipLatLng uses the IPStack api to convert an IP address into
// a latitude and longitude, sending the result on the channel
func ipLatLng(apikey, name, ip string, ch chan MarkResponse) {
	if ip == "" {
		ch <- MarkResponse{Marker{Name: name}, errors.New("no IP provided")}
		return
	}
	query := fmt.Sprintf("https://freegeoip.app/json/%s", ip)
	resp, err := http.Get(query)
	if err != nil {
		ch <- MarkResponse{Marker{Name: name}, err}
		return
	}
	defer resp.Body.Close()
	// freegeoip.app will give 403 if we've made more than 15,000 queries per hour.
	// Unlikely, yes, but good to be careful.
	if resp.StatusCode == 403 {
		// this is logged to stderr, so it should be picked up by the cron daemon
		fmt.Fprintf(os.Stderr, "The request to freegeoip.app returned %s", resp.Status)
	}

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

// read is just a wrapper to read a file.
func read(fname string) ([]byte, error) {
	f, err := os.Open(fname)
	defer f.Close()
	if err != nil {
		return []byte{}, err
	}
	return ioutil.ReadAll(f)
}
