package junos_helpers

import (
	"encoding/xml"
	"fmt"
	"log"
	"strings"
	"sync"

	driver "github.com/davedotdev/go-netconf/drivers/driver"
	sshdriver "github.com/davedotdev/go-netconf/drivers/ssh"
	"golang.org/x/crypto/ssh"
)

const batchGroupStrXML = `<load-configuration action="merge" format="xml">
<configuration>
%s
</configuration>
</load-configuration>
`
const batchGetGroupStr = `<get-configuration database="committed" format="text" >
<configuration>
<groups></groups>
</configuration>
</get-configuration>
`
const batchGetGroupXMLStr = `<get-configuration>
  <configuration>
  <groups></groups>
  </configuration>
</get-configuration>
`
const batchValidateCandidate = `<validate> 
<source> 
	<candidate/> 
</source> 
</validate>`
const batchReadWrapper = `<configuration>%s</configuration>`

const batchCommitStr = `<commit/>`

const batchDeletePayload = `<groups operation="delete"><name>%[1]s</name></groups><apply-groups operation="delete">%[1]s</apply-groups>`

const batchDeleteStr = `<edit-config>
	<target>
		<candidate/>
	</target>
	<default-operation>none</default-operation> 
	<config>
		<configuration>
			%s
		</configuration>
	</config>
</edit-config>`

var (
	batchConfigReplacer = strings.NewReplacer("<configuration>", "", "</configuration>", "")
)

// BatchGoNCClient type for storing data and wrapping functions
type BatchGoNCClient struct {
	Driver      driver.Driver
	Lock        sync.RWMutex
	readCache   string
	writeCache  string
	deleteCache string
}

// Close is a functional thing to close the Driver
func (g *BatchGoNCClient) Close() error {
	g.Driver = nil
	return nil
}

// ReadGroup is a helper function
func (g *BatchGoNCClient) ReadGroup(applygroup string) (string, error) {
	g.Lock.Lock()
	g.Lock.Unlock()
	return "", nil
}

// UpdateRawConfig deletes group data and replaces it (for Update in TF)
func (g *BatchGoNCClient) UpdateRawConfig(applygroup string, netconfcall string, commit bool) (string, error) {
	g.Lock.Lock()
	g.deleteCache += fmt.Sprintf(batchDeletePayload, applygroup)
	g.writeCache += batchConfigReplacer.Replace(netconfcall)
	g.Lock.Unlock()
	return "", nil
}

// DeleteConfig is a wrapper for driver.SendRaw()
func (g *BatchGoNCClient) DeleteConfig(applygroup string) (string, error) {
	g.Lock.Lock()
	g.Lock.Unlock()
	return "", nil
}

// DeleteConfigNoCommit is a wrapper for driver.SendRaw()
// Does not provide mandatory commit unlike DeleteConfig()
func (g *BatchGoNCClient) DeleteConfigNoCommit(applygroup string) (string, error) {
	g.Lock.Lock()
	// Due to the way this functions any resource deletes will only happen client site
	// however a full destroy will be committed to the server.
	g.deleteCache += fmt.Sprintf(batchDeletePayload, applygroup)

	g.Lock.Unlock()
	return "", nil
}

// SendCommit is a wrapper for driver.SendRaw()
func (g *BatchGoNCClient) SendCommit() error {

	g.Lock.Lock()

	if err := g.Driver.Dial(); err != nil {
		return err
	}
	//These are net new adds. Need to ensure we write these first
	if g.writeCache != "" {
		groupCreateString := fmt.Sprintf(batchGroupStrXML, g.writeCache)
		// So on the commit we are going to send our entire write cache, if we get any load error
		// we return the full xml error response and exit
		batchWriteReply, err := g.Driver.SendRaw(groupCreateString)
		if err != nil {
			errInternal := g.Driver.Close()
			g.Lock.Unlock()
			return fmt.Errorf("driver error: %+v, driver close error: %+s", err, errInternal)
		}
		// I am doing string checks simply because it is most likely more efficient
		// than loading in through an xml parser
		if strings.Contains(batchWriteReply.Data, "operation-failed") {
			return fmt.Errorf("failed to write batch configuration %s", batchWriteReply.Data)
		}
	}
	if g.deleteCache != "" {
		backDeleteString := fmt.Sprintf(batchDeleteStr, g.deleteCache)
		// So on the commit we are going to send our entire write cache, if we get any load error
		// we return the full xml error response and exit
		batchDeleteReply, err := g.Driver.SendRaw(backDeleteString)
		if err != nil {
			errInternal := g.Driver.Close()
			g.Lock.Unlock()
			return fmt.Errorf("driver error: %+v, driver close error: %+s", err, errInternal)
		}
		// I am doing string checks simply because it is most likely more efficient
		// than loading in through an xml parser
		if strings.Contains(batchDeleteReply.Data, "operation-failed") {
			return fmt.Errorf("failed to write batch delete %s", batchDeleteReply.Data)
		}
	}
	// we have loaded the full configuration without any error
	// before we can commit this we are going to do a commit check
	// if it fails we return the full xml error
	commitCheckReply, err := g.Driver.SendRaw(batchValidateCandidate)
	if err != nil {
		errInternal := g.Driver.Close()
		g.Lock.Unlock()
		return fmt.Errorf("driver error: %+v, driver close error: %+s", err, errInternal)
	}

	// I am doing string checks simply because it is most likely more efficient
	// than loading in through an xml parser
	if !strings.Contains(commitCheckReply.Data, "commit-check-success") {
		return fmt.Errorf("candidate commit check failed %s", commitCheckReply.Data)
	}
	if _, err = g.Driver.SendRaw(batchCommitStr); err != nil {
		g.Lock.Unlock()
		return err
	}
	g.Lock.Unlock()
	return nil
}

// MarshalGroup accepts a struct of type X and then marshals data onto it
func (g *BatchGoNCClient) MarshalGroup(id string, obj interface{}) error {

	reply, err := g.ReadRawGroup(id)
	if err != nil {
		return err
	}

	nodes, err := findGroupInDoc(reply, fmt.Sprintf("//configuration/groups[name='%s']", id))
	if err != nil {
		return err
	}

	if len(nodes) > 1 {
		return fmt.Errorf("%s returned an invalid read cache node count %d", len(nodes))
	}
	var nodeXML string
	if len(nodes) == 0 {
		// Okay so this means we can't fine the node in the reply cache XML.
		// the question is, should we check the write cache? could be a net new element
		subNodes, err := findGroupInDoc(g.writeCache, fmt.Sprintf("//groups[name='%s']", id))
		if err != nil {
			return err
		}
		if len(subNodes) > 1 || len(subNodes) == 0 {
			return fmt.Errorf("%s returned an invalid write cache node count %d", len(subNodes))
		}
		nodeXML = fmt.Sprintf(batchReadWrapper, subNodes[0].OutputXML(true))
	} else {
		nodeXML = fmt.Sprintf(batchReadWrapper, nodes[0].OutputXML(true))
	}
	if err := xml.Unmarshal([]byte(nodeXML), &obj); err != nil {
		return err
	}

	return nil
}

// SendTransaction is a method that unnmarshals the XML, creates the transaction and passes in a commit
func (g *BatchGoNCClient) SendTransaction(id string, obj interface{}, commit bool) error {
	jconfig, err := xml.Marshal(obj)

	if err != nil {
		return err
	}
	if id != "" {
		_, err = g.UpdateRawConfig(id, string(jconfig), commit)
	} else {
		if _, err := g.SendRawConfig(string(jconfig), commit); err != nil {
			return err
		}
	}
	return nil
}

// SendRawConfig is a wrapper for driver.SendRaw()
func (g *BatchGoNCClient) SendRawConfig(netconfcall string, commit bool) (string, error) {
	g.Lock.Lock()
	// we need to strip off the <configuration> blocks since we want to send this \
	// as one large configuration push
	g.writeCache += batchConfigReplacer.Replace(netconfcall)
	g.Lock.Unlock()
	return "", nil
}

// ReadRawGroup is a helper function
func (g *BatchGoNCClient) ReadRawGroup(applygroup string) (string, error) {

	g.Lock.Lock()
	output := g.readCache
	// We only want to capture all the groups the first execution
	// after that we depend up on this cache for the duration of the execution
	if g.readCache == "" {
		if err := g.Driver.Dial(); err != nil {
			log.Fatal(err)
		}
		reply, err := g.Driver.SendRaw(batchGetGroupXMLStr)
		if err != nil {
			errInternal := g.Driver.Close()
			g.Lock.Unlock()
			return "", fmt.Errorf("driver error: %+v, driver close error: %+s", err, errInternal)
		}
		if err := g.Driver.Close(); err != nil {
			return "", err
		}
		g.readCache = reply.Data
		output = g.readCache
	}
	g.Lock.Unlock()
	return output, nil
}

// NewBatchClient returns gonetconf new client driver
func NewBatchClient(username string, password string, sshkey string, address string, port int) (NCClient, error) {

	// Dummy interface var ready for loading from inputs
	var nconf driver.Driver

	d := driver.New(sshdriver.New())

	nc := d.(*sshdriver.DriverSSH)

	nc.Host = address
	nc.Port = port

	// SSH keys takes priority over password based
	if sshkey != "" {
		nc.SSHConfig = &ssh.ClientConfig{
			User: username,
			Auth: []ssh.AuthMethod{
				publicKeyFile(sshkey),
			},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		}
	} else {
		// Sort yourself out with SSH. Easiest to do that here.
		nc.SSHConfig = &ssh.ClientConfig{
			User:            username,
			Auth:            []ssh.AuthMethod{ssh.Password(password)},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		}
	}

	nconf = nc

	return &BatchGoNCClient{Driver: nconf}, nil
}
