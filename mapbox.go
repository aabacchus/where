/* Copyright 2021 Ben Fuller
 * Apache License, Version 2.0
 * See LICENSE file for copyright and licence details.
 */

package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
)

// MapboxDetails holds the basic credentials to generate mapbox content.
type MapboxDetails struct {
	Uname   string
	Style   string
	Apikey  string
	Padding int
}

// MapboxStatic creates a static image of a map
// with markers represented by m
func MapboxStatic(m []Marker, fname string, mbox MapboxDetails) error {
	baseURL := "https://api.mapbox.com/"
	query := fmt.Sprintf("styles/v1/%s/%s/static/", mbox.Uname, mbox.Style)
	// it is possible to define a bbox (which defines the field of view)
	// in the format [minlng,minlat,maxlng,maxlat]
	// rather than "auto" which fits the field of view to the markers on the map
	// the field after auto is the dimensions of the png image
	// the parameters at the end (after the ?) are the access_token (required)
	// and padding (optional)
	suffix := fmt.Sprintf("/auto/800x720?padding=%d&access_token=%s", mbox.Padding, mbox.Apikey)

	var markersMapbox string
	for _, mark := range m {
		// don't plot places with no data
		if mark.Lat == 0 && mark.Lng == 0 {
			continue
		}
		markersMapbox += MarkerToMapbox(mark, "", "aa0500") + ","
	}
	// if there were none left after removing those at (0,0), return
	if len(markersMapbox) == 0 {
		return fmt.Errorf("no markers with non-zero location")
	}
	// remove the final comma
	markersMapbox = markersMapbox[:len(markersMapbox)-1]

	// make the request
	imgP, err := http.Get(baseURL + query + markersMapbox + suffix)
	if err != nil {
		return err
	}
	defer imgP.Body.Close()
	bytes, _ := ioutil.ReadAll(imgP.Body)
	// save the image
	f, err := os.Create(fname)
	f.Write(bytes)
	return err
}

// MarkerToMapbox takes a Marker which has a position,
// and optionally a label (a-z, 0-99, or a Makl icon) and color
// (if you don't want these, provide empty strings)
// and returns the correctly formatted marker for Mapbox
func MarkerToMapbox(m Marker, label string, color string) string {
	if label != "" {
		label = "-" + label
	}
	if color != "" {
		color = "+" + color
	}
	return fmt.Sprintf("pin-s%s%s(%f,%f)", label, color, m.Lng, m.Lat)
}
