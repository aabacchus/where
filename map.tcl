package require sqlite3

proc httpget {url} {
	set fl [open |[list curl -fSs $url] rb]
	try {
		set res [read $fl]
		close $fl
		set err {}
	} trap CHILDSTATUS {results options} {
		set err [lindex [split [dict get $options -errorinfo] "\n"] 0]
	}
	return [list $res $err]
}

proc json {blob filter} {
	exec jq -r $filter << $blob
}

proc parse_creds {fname} {
	set f [open $fname r]
	set t [read $f]
	close $f

	set res [split [json $t ".k,.mboxa,.mboxp,.mboxs,.mboxu"] "\n"]
	lassign $res ipkey apikey padding style uname
	set creds [dict create \
		apikey $apikey \
		padding $padding \
		style $style \
		uname $uname]

	return [list $ipkey $creds]
}

proc lookup_ip {key ip} {
	if {$ip == {}} {
		puts "ERROR no ip given"
		return
	}
	lassign [httpget "http://api.ipstack.com/$ip?access_key=$key"] resp err
	if {$err ne {}} {
		puts "ERROR with ip '$ip': $err"
		return
	}

	set err [json $resp ".error.info"]
	if {$err ne "null"} {
		puts "ERROR: $err"
		return
	}

	set ll [split [json $resp ".latitude,.longitude"] "\n"]
	# TODO: check for 0,0.
	return $ll
}

proc init_db {} {
	sqlite3 db ips.sqlite
	db timeout 2000
	db eval {CREATE TABLE IF NOT EXISTS ips(ip TEXT PRIMARY KEY, lat, long TEXT);}
	db eval {CREATE TABLE IF NOT EXISTS names(name TEXT PRIMARY KEY, ip TEXT);}
	return db
}

proc check_ip_cache {db ip} {
	set ll [db eval {SELECT lat,long FROM ips WHERE ip=$ip}]
	return $ll
}

proc update_ip_cache {db ip lat long} {
	db eval {INSERT INTO ips VALUES($ip, $lat, $long)}
}

proc store_ip_to_ll {db ipkey ip} {
	set cached [check_ip_cache $db $ip]
	if {$cached ne {}} {
		return $cached
	} else {
		set ll [lookup_ip $ipkey $ip]
		if {$ll eq {}} {
			return
		}
		update_ip_cache $db $ip {*}$ll
		return $ll
	}
}

proc get_lls {db} {
	set lls {}
	$db eval {SELECT DISTINCT lat,long FROM ips,names WHERE ips.ip = names.ip} {
		lappend lls [list $lat $long]
	}
	return $lls
}

proc store_name {db name ip} {
	# maintain a table of most recent ips for each user. plot these, using the ips table as a cache.
	if {[$db exists {SELECT 1 FROM names WHERE name=$name}]} {
		$db eval {UPDATE names SET ip=$ip WHERE name=$name}
	} else {
		$db eval {INSERT INTO names VALUES($name,$ip)}
	}
}

proc get_who {} {
	set res [exec who --ips]
	return $res
}

proc parse_who {txt} {
	# first split into a dictionary: keys are usernames, values are lists of possible IPs.
	set parsed {}
	set lines [split $txt "\n"]

	foreach line $lines {
		if {$line eq {}} break

		set words [split $line]
		set i -1
		foreach word $words {
			if {$word ne {}} {
				incr i
				if {$i == 0} {
					set name $word
					continue
				}
				if {$i == 4} {
					set word [string trim $word ()]
					# remove :port suffix
					set s [string first : $word]
					if {$s != -1} {
						set word [string range $word 0 [expr {$s-1}]]
					}
					dict lappend parsed $name $word
					break
				}
			}
		}
	}

	# check the duplicates and pick the first which is an ip.
	# save them to a new dict so that keys without any valid ips are removed.
	set new {}
	dict for {key vals} $parsed {
		foreach v $vals {
			if {[regexp {^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$} $v]} {
				dict set new $key $v
				break
			}
		}
	}
	set parsed $new

	set parsed [dict filter $parsed script {name ip} {filter_opted_in $name $ip}]

	return $parsed
}

proc filter_opted_in {name ip} {
	return [expr {[file exists [file join /home $name .here]] ||
		[file exists [file join /home $name .somewhere]]}]
}

proc dynamic_map {lls fname} {
	# build a javascript file.
	set f [open $fname w]

	puts $f {var map = L.map('map').setView([20,10], 1);
L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
        maxzoom: 19,
        attribution: '&copy; <a href="http://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'}).addTo(map);}

	foreach ll $lls {
		puts $f "L.marker(\[[join $ll ,]\]).addTo(map);"
	}

	close $f
}

proc static_map {lls fname creds} {
	# construct an http request to get a map image.
	set url "https://api.mapbox.com/styles/v1/[dict get $creds uname]/[dict get $creds style]/static/"
	set suffix "/auto/800x720?padding=[dict get $creds padding]&access_token=[dict get $creds apikey]"

	set color aa0500
	set markers {}
	foreach ll $lls {
		# NB they require long,lat format.
		lappend markers "pin-s+${color}([join [lreverse $ll] ,])"
	}

	if {[llength $markers] == 0} {
		puts "no markers to plot"
		return
	}

	set req "${url}[join $markers ,]${suffix}"
	lassign [httpget $req] res err
	if {$err ne {}} {
		puts "ERROR: $err"
		return
	}

	set f [open $fname w]
	fconfigure $f -translation binary
	puts -nonewline $f $res
	close $f
}

proc main {} {
	lassign [parse_creds creds.json] ipkey creds

	set parsed [parse_who [get_who]]

	set db [init_db]

	# update the databases
	dict for {name ip} $parsed {
		store_ip_to_ll $db $ipkey $ip
		store_name $db $name $ip
	}
	# query the databases to get lls to plot
	set lls [get_lls $db]

	# done with this now
	$db close

	dynamic_map $lls "dynamic.js"
	static_map $lls "map.png" $creds
}

# TODO: get arg of a DESTDIR and write map.html and dynamic.html there rather than relying on a shell script.
main
