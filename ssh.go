package main

import (
	"fmt"
	"io/ioutil"

	"golang.org/x/crypto/ssh"
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

// connectToSSH dials the server and opens a session. Callers must close both the session and
// the client, otherwise the underlying TCP connection is leaked.
func connectToSSH(username string, keyfile string, server string) (*ssh.Client, *ssh.Session, error) {
	k := new(keychain)
	k.loadPEM(keyfile)

	auths := []ssh.AuthMethod{ssh.PublicKeys(k.keys...)}

	config := &ssh.ClientConfig{
		User:            username,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	client, err := ssh.Dial("tcp", server, config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to dial %s: %w", server, err)
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, nil, err
	}

	return client, session, nil
}
