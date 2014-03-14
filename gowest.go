package main

import (
  "io"
  "code.google.com/p/go.crypto/ssh"
  "log"
  "io/ioutil"
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



func main() {
  k := new(keychain)
  // Add path to id_rsa file
  err := k.loadPEM("/Users/amrk/.ssh/id_rsa")

  config := &ssh.ClientConfig{
        User: "amrk",
        Auth: []ssh.ClientAuth{
            ssh.ClientAuthKeyring(k),
        },
    }
    client, err := ssh.Dial("tcp", "127.0.0.1:29418", config)
    if err != nil {
        panic("Failed to dial: " + err.Error())
    }
    defer client.Close()
    // Create a session
    session, err := client.NewSession()
    if err != nil {
        log.Fatalf("unable to create session: %s", err)
    }
    defer session.Close()

    io.WriteString(session.Stdout, "gerrit stream-events\n")

}