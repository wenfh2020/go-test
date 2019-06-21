package main

// watch config data from zk

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/samuel/go-zookeeper/zk"
	"github.com/spf13/viper"
)

var (
	errInvalidPath           = errors.New("invalid path")
	errConfigCenterNotInited = errors.New("config center not inited")
	defaultLocalCacheDir     = "/usr/local/test/config_center/local_cache"
	connTimeOut              = 8 * time.Second //zk conn timeout
)

// ConfigModule conf file handle
type ConfigModule struct {
	modulePath    string       //zk config module
	localCacheDir string       //dir where cache all config files
	localDir      string       //local config dir
	filePath      string       //config file path
	prevCfgBuf    []byte       //config buf get from zk last time
	config        *viper.Viper // config obj ptr
}

// load buf into config
func (m *ConfigModule) loadFromBuf(buf []byte) error {
	fmt.Println("ConfigModule::loadFromBuf...")

	// check same content
	if reflect.DeepEqual(m.prevCfgBuf, buf) {
		fmt.Println("no need to update...")
		return nil
	}

	equal := false

	// check local to remote
	if pathExists(m.filePath) {
		data, err := ioutil.ReadFile(m.filePath)
		if err != nil {
			fmt.Println(err)
			return nil
		}

		if reflect.DeepEqual(data, buf) {
			equal = true
		}
	}

	if !equal {
		// create temp file
		file, err := ioutil.TempFile(m.localDir, "tmp")
		if err != nil {
			fmt.Println("create tmp file tmp", err)
			return err
		}
		defer file.Close()

		file.Write(buf)

		// add ext to temp file
		dir, filename := path.Split(file.Name())
		tempPath := path.Join(dir, filename+path.Ext(m.filePath))
		if err = os.Rename(file.Name(), tempPath); err != nil {
			fmt.Println("rename file fail", err, file.Name(), tempPath)
			return err
		}

		// check content
		v := viper.New()
		v.SetConfigFile(tempPath)
		if err = v.ReadInConfig(); err != nil {
			fmt.Println(err)
			return err
		}

		// replace old config
		if err = os.Rename(tempPath, m.filePath); err != nil {
			fmt.Println("rename file fail", err, tempPath, m.filePath)
			return err
		}
	}

	m.config.SetConfigFile(m.filePath)
	if err := m.config.ReadInConfig(); err != nil {
		fmt.Println(err)
		return err
	}

	fmt.Println("update config success", m.filePath)
	m.prevCfgBuf = buf
	return nil
}

// pathExists check path
func pathExists(path string) bool {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsExist(err) {
			return true
		}
		return false
	}
	return true
}

// initConfigModule create module obj
func initConfigModule(mPath, cacheDir string) (config *ConfigModule, err error) {
	fmt.Println("initConfigModule...")

	if cacheDir == "" {
		cacheDir = defaultLocalCacheDir
	} else {
		p, err := filepath.Abs(cacheDir)
		if err != nil {
			cacheDir = defaultLocalCacheDir
		} else {
			cacheDir = p
		}
	}
	cacheDir = strings.TrimRight(cacheDir, "/")
	p := path.Join(cacheDir, mPath)

	// check and create dir
	dir, _ := path.Split(p)
	if !pathExists(dir) {
		if err = os.MkdirAll(dir, os.ModePerm); err != nil {
			fmt.Println("mkdir fail", dir, err)
			return nil, err
		}
		fmt.Println("mkdir success!", dir)
	}

	v := viper.New()
	v.SetConfigFile(p)

	v.WatchConfig()
	v.OnConfigChange(func(e fsnotify.Event) {
		fmt.Println("file change..", e.Name)
	})

	module := &ConfigModule{
		modulePath:    mPath,
		localCacheDir: cacheDir,
		localDir:      dir,
		filePath:      p,
		config:        v,
	}

	return module, nil
}

// ConfigCenter config center
type ConfigCenter struct {
	zkConn    *zk.Conn                 // zookeeper connect
	mutex     *sync.Mutex              // lock
	configMap map[string]*ConfigModule // config module
}

// ConfigCenter obj
var configCenter *ConfigCenter

// InitConfigCenter init config center
func InitConfigCenter() *ConfigCenter {
	configCenter = &ConfigCenter{
		mutex:     &sync.Mutex{},
		configMap: make(map[string]*ConfigModule),
	}
	return configCenter
}

func (c *ConfigCenter) getZkHosts() []string {
	var hosts []string
	for i := 1; i < 10; i++ {
		hosts = append(hosts, fmt.Sprintf("zookeeper%02d.topnews.com:2181", i))
	}
	return hosts
}

func (c *ConfigCenter) initZk() (*zk.Conn, error) {
	conn, _, err := zk.Connect(c.getZkHosts(), connTimeOut)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	return conn, nil
}

func (c *ConfigCenter) watchNodeDataChange(modulePath string, conn *zk.Conn) {
	fmt.Println("ConfigCenter::watchNodeDataChange...")

	for {
		zkPath := c.getModuleZkPath(modulePath)
		_, _, ch, _ := conn.GetW(zkPath)
		e := <-ch
		fmt.Println(e)

		v, s, err := conn.Get(zkPath)
		if err != nil {
			fmt.Println(s, err)
			return
		}

		c.mutex.Lock()
		defer c.mutex.Unlock()

		module, ok := c.configMap[modulePath]
		if !ok {
			fmt.Println("can not find config of path:", modulePath)
			continue
		}

		if err := module.loadFromBuf(v); err != nil {
			fmt.Println(err)
			continue
		}

		fmt.Println("file change success!")
	}
}

func (c *ConfigCenter) getModuleZkPath(modulePath string) string {
	return fmt.Sprintf("/config_center%s", modulePath)
}

func (c *ConfigCenter) getModule(moudlePath, localCacheDir string) (*viper.Viper, error) {
	fmt.Println("ConfigCenter::getModule", moudlePath, localCacheDir)

	v, ok := c.configMap[moudlePath]
	if ok {
		return v.config, nil
	}

	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.zkConn == nil {
		conn, err := c.initZk()
		if err != nil {
			fmt.Println("init zk fail! err:", err)
			return nil, err
		}
		c.zkConn = conn
	}

	module, err := initConfigModule(moudlePath, localCacheDir)
	if err != nil {
		fmt.Println("init confing module fail: ", module)
		return nil, err
	}

	zkPath := c.getModuleZkPath(moudlePath)
	fmt.Println("zk module path:", zkPath)

	// get data from zk
	value, _, err := c.zkConn.Get(zkPath)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	if err := module.loadFromBuf(value); err != nil {
		fmt.Println("load from buf fail:", err)
		return nil, err
	}

	// watch data.
	go c.watchNodeDataChange(moudlePath, c.zkConn)

	c.configMap[moudlePath] = module
	fmt.Println("add new module config:", moudlePath, localCacheDir)
	return module.config, nil
}

// GetModule init module by path
func GetModule(modulePath, localCacheDir string) (*viper.Viper, error) {
	fmt.Println("GetModule...")

	if configCenter == nil {
		panic("pls call InitConfigCenter()")
	}

	if modulePath == "" || modulePath[0] != '/' {
		fmt.Println(errInvalidPath.Error())
		return nil, errInvalidPath
	}

	return configCenter.getModule(modulePath, localCacheDir)
}
