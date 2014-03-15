package main

import (
	"bufio"
	"code.google.com/p/go.crypto/ssh"
	"io"
	"io/ioutil"
	"log"
)

type keychain struct {
	keys []ssh.Signer
}

func (k *keychain) Key(i int) (ssh.PublicKey, error) {
	if i < 0 || i >= len(k.keys) {
		return nil, nil
	}
	return k.keys[i].PublicKey(), nil
}

func (k *keychain) Sign(i int, rand io.Reader, data []byte) (sig []byte, err error) {
	return k.keys[i].Sign(rand, data)
}

func (k *keychain) add(key ssh.Signer) {
	k.keys = append(k.keys, key)
}

func (k *keychain) loadPEM(file string) error {
	buf, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}
	key, err := ssh.ParsePrivateKey(buf)
	if err != nil {
		return err
	}
	k.add(key)
	return nil
}

func listenToGerrit(username string, keyfile string, server string) (chan string, error) {

	k := new(keychain)
	// Add path to id_rsa file
	k.loadPEM(keyfile)

	config := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.ClientAuth{
			ssh.ClientAuthKeyring(k),
		},
	}
	client, err := ssh.Dial("tcp", server, config)
	if err != nil {
		log.Fatalf("Failed to dial: %s", err.Error())
		return nil, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		log.Fatalf("unable to create session: %s", err)
		return nil, err
	}
	defer session.Close()

	log.Printf("Connected to %s - listening for stream events...", server)

	reader, _ := session.StderrPipe()

  // if I don't set a buffer, I dont see anything get returned.
	streamChannel := make(chan string, 1000)

	session.Start("gerrit --help")
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		streamChannel <- line
	}

	return streamChannel, nil
}
