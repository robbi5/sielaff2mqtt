package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/godbus/dbus/v5"
)

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func main() {
	log.Println("Hello vending machine!")

	mqttBrokerPtr := flag.String("broker", getEnv("MQTT_BROKER", "tcp://localhost:1883"), "MQTT Broker hostname/ip")
	mqttUsernamePtr := flag.String("username", getEnv("MQTT_USERNAME", "sielaff2mqtt"), "MQTT Broker username")
	mqttPasswordPtr := flag.String("password", getEnv("MQTT_PASSWORD", ""), "MQTT Broker password")
	mqttPrefixPtr := flag.String("prefix", getEnv("MQTT_PREFIX", "sielaff2mqtt"), "MQTT prefix")
	flag.Parse()

	dbusConn, err := dbus.ConnectSystemBus()
	if err != nil {
		log.Fatal(err)
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(*mqttBrokerPtr)
	opts.SetUsername(*mqttUsernamePtr)
	opts.SetPassword(*mqttPasswordPtr)
	opts.SetKeepAlive(30 * time.Second)
	opts.SetDefaultPublishHandler(func(client mqtt.Client, msg mqtt.Message) {
		log.Printf("TOPIC: %s\n", msg.Topic())
		log.Printf("MSG: %s\n", msg.Payload())
	})
	opts.SetPingTimeout(1 * time.Second)
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(10 * time.Second)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(10 * time.Second)
	opts.SetWriteTimeout(60 * time.Second)
	opts.SetClientID("")

	opts.SetOnConnectHandler(
		func(c mqtt.Client) {
			log.Println("Connected to MQTT")

			// homeassistant auto discovery
			vendHassConfigTopic := "homeassistant/binary_sensor/matematvend/config"
			token := c.Publish(vendHassConfigTopic, 2, true, fmt.Sprintf(`{"name":"Matemat Vending Enabled", "availability_topic": "%[1]s/matematvend/availability", "unique_id": "matematvend", "state_topic": "%[1]s/matematvend/state", "device_class": "running"}`, *mqttPrefixPtr))
			token.Wait()

			token = c.Publish(*mqttPrefixPtr+"/matematvend/availability", 2, true, "online")
			token.Wait()

			// homeassistant auto discovery
			serviceMenuHassConfigTopic := "homeassistant/binary_sensor/matematservicemenu/config"
			token = c.Publish(serviceMenuHassConfigTopic, 2, true, fmt.Sprintf(`{"name":"Matemat Service Menu", "availability_topic": "%[1]s/matematservicemenu/availability", "unique_id": "matematservicemenu", "state_topic": "%[1]s/matematservicemenu/state"}`, *mqttPrefixPtr))
			token.Wait()

			token = c.Publish(*mqttPrefixPtr+"/matematservicemenu/availability", 2, true, "online")
			token.Wait()

			// homeassistant auto discovery
			coolingTemperatureHassConfigTopic := "homeassistant/sensor/matematcoolingtemperature/config"
			token = c.Publish(coolingTemperatureHassConfigTopic, 2, true, fmt.Sprintf(`{"name":"Matemat Cooling Temperature", "availability_topic": "%[1]s/matematcoolingtemperature/availability", "unique_id": "matematcoolingtemperature", "state_topic": "%[1]s/matematcoolingtemperature/state", "device_class": "temperature", "unit_of_measurement": "°C"}`, *mqttPrefixPtr))
			token.Wait()

			token = c.Publish(*mqttPrefixPtr+"/matematcoolingtemperature/availability", 2, true, "online")
			token.Wait()

			log.Println("MQTT topics published")
		},
	)

	client := mqtt.NewClient(opts)

	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatal(token.Error())
	}

	// list of dbus match signals
	var matchSensorValue = []dbus.MatchOption{
		dbus.WithMatchObjectPath("/MachineControl"),
		dbus.WithMatchInterface("com.sielaff.siline.machine.MachineControl"),
		dbus.WithMatchMember("sensorValue"),
	}
	if err = dbusConn.AddMatchSignal(matchSensorValue...); err != nil {
		log.Fatal(err)
	}

	var matchVendValue = []dbus.MatchOption{
		dbus.WithMatchObjectPath("/"),
		dbus.WithMatchInterface("com.sielaff.siline.payment"),
		dbus.WithMatchMember("vendEnabled"),
	}
	if err = dbusConn.AddMatchSignal(matchVendValue...); err != nil {
		log.Fatal(err)
	}

	var matchServiceMenuValue = []dbus.MatchOption{
		dbus.WithMatchObjectPath("/"),
		dbus.WithMatchInterface("com.sielaff.siline.hmi"),
		dbus.WithMatchMember("serviceMenuActivated"),
	}
	if err = dbusConn.AddMatchSignal(matchServiceMenuValue...); err != nil {
		log.Fatal(err)
	}

	sensorChan := make(chan *dbus.Signal, 10)
	dbusConn.Signal(sensorChan)
	// coroutine to handle dbus signal
	go func() {
		log.Println("dbus signal handler started")
		for v := range sensorChan {
			log.Println(v)

			if v.Name == "com.sielaff.siline.machine.MachineControl.sensorValue" {
				if v.Body[0] != "Cooling" {
					log.Println("ignored sensorValue", v.Body[0])
					continue
				}
				if v.Body[1] != "product.temperature" {
					log.Println("ignored sensorValue path", v.Body[1])
					continue
				}

				var temp float64
				v.Body[2].(dbus.Variant).Store(&temp)
				log.Println("cooling product.temperature", fmt.Sprintf("%.2f", temp))

				token := client.Publish(*mqttPrefixPtr+"/matematcoolingtemperature/state", 2, false, fmt.Sprintf("%.2f", temp))
				token.Wait()
			} else if v.Name == "com.sielaff.siline.payment.vendEnabled" {
				var enabled bool = v.Body[0].(bool)
				log.Println("vendEnabled", fmt.Sprintf("%t", enabled))

				var state string
				if enabled {
					state = "on"
				} else {
					state = "off"
				}
				token := client.Publish(*mqttPrefixPtr+"/matematvend/state", 2, false, state)
				token.Wait()
			} else if v.Name == "com.sielaff.siline.hmi.serviceMenuActivated" {
				var active bool = v.Body[0].(bool)
				log.Println("service menu active", fmt.Sprintf("%t", active))

				var state string
				if active {
					state = "on"
				} else {
					state = "off"
				}
				token := client.Publish(*mqttPrefixPtr+"/matematservicemenu/state", 2, false, state)
				token.Wait()
			}
		}
	}()

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Everything is set up")
	<-done

	log.Println("Server Stopped")

	// unsubscribe dbus
	dbusConn.RemoveMatchSignal(matchSensorValue...)
	dbusConn.RemoveMatchSignal(matchVendValue...)

	// unsubscribe mqtt
	token := client.Publish(*mqttPrefixPtr+"/matematvend/availability", 2, true, "offline")
	token.Wait()

	token = client.Publish(*mqttPrefixPtr+"/matematservicemenu/availability", 2, true, "offline")
	token.Wait()

	token = client.Publish(*mqttPrefixPtr+"/matematcoolingtemperature/availability", 2, true, "offline")
	token.Wait()

	log.Println("All Devices Unsubscribed")

	client.Disconnect(250)

	dbusConn.Close()

	time.Sleep(1 * time.Second)
}
