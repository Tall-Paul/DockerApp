package main

import (
	"dockerap/monitor"
	"dockerap/server"
	"dockerap/store"
	"flag"
	"log"
)

var (
	modeFlag = flag.String("mode", "server", "Operating mode: 'server' or 'monitor'")
)

func main() {
	flag.Parse()

	if *modeFlag == "server" {
		s, err := store.NewStore("./dockerapp.db")
		if err != nil {
			log.Fatalf("Failed to create store: %s", err)
		}
		defer s.Close()
		s.InitSchema()

		srv := server.NewServer(s)
		srv.Run()

	} else if *modeFlag == "monitor" {
		mon, err := monitor.NewMonitor()
		if err != nil {
			log.Fatalf("Failed to create monitor: %s", err)
		}
		mon.Run()

	} else {
		log.Fatalf("Unknown mode: %s", *modeFlag)
	}
}