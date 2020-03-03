package main

import (
	"flag"
	"os"
	"fmt"
    "time"
    "context"
	"io/ioutil"
	//ansibler "github.com/apenella/go-ansible"
	//"bytes"
	//"strings"
	"github.com/tidwall/gjson"
    "github.com/Ullaakut/nmap"
	"log"
	"net"
	"net/http"
	"strings"
)

type Host struct {
	Hostname string
	Port string
}

func main() {
	//vars
	var assets = make(map[string]*Host)
	subnet := flag.String("subnet-cidr", "", "Specify subnet to be scanned")
    ports := flag.String("p","22","Specify on wich ports SSH migt be listening on")
	username := flag.String("u","root","Specify an username that has access to all machines")
	password := flag.String("password","","Set a password for defined username")
	latitude := flag.String("site-lat","","Override latitude discovery for a site")
	longitude := flag.String("site-long","","Override longitude discovery for a site")
    flag.Parse()

    // setup nmap scanner in order to discover active hosts
	log.Println("[*] Setting Up NSE engine")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()
    scanner, err := nmap.NewScanner(
        nmap.WithTargets(*subnet),
        nmap.WithPorts(*ports),
        nmap.WithContext(ctx),
    )
	check(err)
    result, warnings, err := scanner.Run()
    check(err)
    if warnings != nil {
        fmt.Printf("Warnings: \n %v", warnings)
    }
	log.Println("[+] Detected network's alive hosts ... diggin' deeper ...")

	//retrive hostnames and insert into a map and perform more accurate scan
    for _, host := range result.Hosts {
		//host down
        if len(host.Ports) == 0 || len(host.Addresses) == 0 {
            continue
        }

		//init loop vars
		host_ipv4 := fmt.Sprintf("%s", host.Addresses[0])
        ptr, _ := net.LookupAddr(host_ipv4)
		assets[host_ipv4] = &Host{}

        for _, port := range host.Ports {
			port_str := fmt.Sprintf("%d",port.ID)
            if(port.Status() == "open") {
				if ptr != nil {
					hostname_ptr_recon := ""
					hostname_ptr_recon = strings.Split(ptr[0], ".")[0]
					assets[host_ipv4].Hostname = hostname_ptr_recon
					assets[host_ipv4].Port = port_str
				} else {
					scanner, err := nmap.NewScanner(
				        nmap.WithTargets(host_ipv4),
				        nmap.WithContext(ctx),
						nmap.WithPorts(port_str),
						nmap.WithScripts("./sbin/nmap/nse/ssh-run-uname"),
						nmap.WithScriptArguments(
							map[string]string{
								"ssh-run.port": port_str,
								"ssh-run.username": *username,
								"ssh-run.password": *password,
							}),
				    )
					result, warnings, err := scanner.Run()
				    check(err)

					if(result.Hosts != nil) {
					    if warnings != nil {
					        fmt.Printf("[!] \n %v", warnings)
					    }
						nmap_hostname := result.Hosts[0].Ports[0].Scripts[0].Output
						if(strings.Contains(nmap_hostname, "Authentication Failed")){
							log.Println("[-] Login failed for host: "+ host_ipv4 + " could not determine hostname, to scan host consider to add a PTR record or provide valid host credentials")
						} else {
							nmap_hostname = strings.Replace(nmap_hostname, "output:", "", -1)
							nmap_hostname = strings.Replace(nmap_hostname, "\n", "", -1)
							nmap_hostname = strings.Replace(nmap_hostname, "\r", "", -1)
							nmap_hostname = strings.Replace(nmap_hostname, " ", "", -1)
							nmap_hostname = strings.Split(nmap_hostname, ".")[0]
							assets[host_ipv4].Hostname = nmap_hostname
							assets[host_ipv4].Port = port_str
						}
					}
				}
        	}
		}
	}
	// deleting elements with SSH problems and with undefined PTR record
	for ip, host := range assets {
		if host.Port == "" && host.Hostname == "" {
			delete(assets, ip)
			log.Println("[-] SSH seems not to be listening on", ip, "at specified ports, and hostname cannot be determined by scanning PTR. Escluding host from Assets.csv")
		}
	}
	// generate .csv that needs to be imported in alienvault
	alienvaultAssetsGenerator(assets, *latitude, *longitude)

}

func check(e error) {
	if e != nil {
		log.Println(e)
		panic(e)
	}
}

//Generate Assets.csv for alienvault
func alienvaultAssetsGenerator(assets map[string]*Host, user_latitude string, user_longitude string) {
	var latitude string
	var longitude string
	log.Println("[*] Retriveing site coordinates...")
	url := "https://freegeoip.app/json/"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Add("accept", "application/json")
	req.Header.Add("content-type", "application/json")
	res, _ := http.DefaultClient.Do(req)
	defer res.Body.Close()
	geoloc, _ := ioutil.ReadAll(res.Body)

	if (user_latitude != "") {
		latitude = user_latitude
	} else {
		log.Println("[*] Detecting latitude...")
		value := gjson.Get(string(geoloc), "latitude")
		latitude = value.String()
		log.Println("[+] LAT: "+latitude)
	}
	if (user_longitude != ""){
		longitude = user_longitude
	} else {
		log.Println("[+] Detecting longitude...")
		value := gjson.Get(string(geoloc), "longitude")
		longitude = value.String()
		log.Println("[+] LNG: "+longitude)
	}

	log.Println("[*] Generating Assets.csv")
	bt := 0
	f, err := os.Create("Assets.csv")
	check(err)
	defer f.Close()
	bc, err := f.WriteString("\"IPs\";\"Hostname\";\"FQDNs\";\"Description\";\"Asset Value\";\"Operating System\";\"Latitude\";\"Longitude\";\"Host ID\";\"External Asset\";\"Device Type\"")
	bt += bc
	check(err)
	for ip, host := range assets {
	   	bc, err := f.WriteString("\n\""+ip+"\";\""+host.Hostname+"\";\"\";\"\";\"2\";\"\";\""+latitude+"\";\""+longitude+"\";\"\";\"\";\"\"")
		bt += bc
	   	check(err)
	}
	f.Sync()
	log.Printf("[+] Alienvault Assets.csv generated in working dir. %d bytes written", bt)
}


//ssh config sshConfigGenerator
// ansible Inventory
//deploy
func sshConfigGenerator(assets map[string]*Host, user string) {
	for ip, host := range assets {
		log.Println("[*] Generating ssh config")
		//vars
		bt := 0
		f, err := os.Create("~/.ssh/config.test")
		check(err)
		defer f.Close()
	   	bc, err := f.WriteString("Host "+host.Hostname+"\n")
	   	bt += bc
	   	check(err)
	   	bc, err = f.WriteString("    User "+user+"\n")
	   	bt += bc
	   	check(err)
	   	bc, err = f.WriteString("    HostName "+ip+"\n")
	   	bt += bc
	   	check(err)
	   	bc, err = f.WriteString("    Port "+host.Port+"\n")
	   	bt += bc
	   	check(err)
	   	bc, err = f.WriteString("\n")
	   	bt += bc
	   	check(err)
		f.Sync()
		log.Println("[+] SSH config generated according to scanned hosts")
	}
}
