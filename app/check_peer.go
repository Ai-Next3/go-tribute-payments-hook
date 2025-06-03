package main

import (
	"log"
	"path"

	"github.com/amarnathcjd/gogram/telegram"
	"go-tribute-api/settings"
)

func checkPeer() {
	if settings.AppID == 0 || settings.AppHash == "" || settings.SessionPath == "" || settings.ForwardTo == 0 {
		log.Println("Error: Missing required Telegram settings")
		return
	}

	log.Println("Using API ID:", settings.AppID)
	log.Println("Using Session Path (folder):", settings.SessionPath)
	log.Println("Checking Peer ID:", settings.ForwardTo)

	config := telegram.ClientConfig{
		AppID:        settings.AppID,
		AppHash:      settings.AppHash,
		Session:      path.Join(settings.SessionPath, "gogram.dat"),
		DisableCache: true,
	}

	client, err := telegram.NewClient(config)
	if err != nil {
		log.Printf("Failed to create client: %v", err)
		return
	}

	log.Println("Connecting to Telegram...")
	if err := client.Connect(); err != nil {
		log.Printf("Failed to connect: %v", err)
		return
	}
	defer client.Disconnect()
	log.Println("Connected successfully!")

	log.Printf("Attempting to find peer with ID %d using GetPeer...\n", settings.ForwardTo)
	peer, err := client.GetPeer(settings.ForwardTo)
	if err != nil {
		log.Printf("!!! FAILED to find peer %d using GetPeer: %v\n", settings.ForwardTo, err)
		peer, err = client.ResolvePeer(settings.ForwardTo)
		if err != nil {
			log.Printf("!!! FAILED to find peer %d using ResolvePeer either: %v\n", settings.ForwardTo, err)
		} else {
			log.Printf("!!! SUCCESS finding peer %d using ResolvePeer: %+v\n", settings.ForwardTo, peer)
		}
	} else {
		log.Printf(">>> SUCCESS finding peer %d using GetPeer: %+v\n", settings.ForwardTo, peer)
	}

	log.Println("Check finished.")
}