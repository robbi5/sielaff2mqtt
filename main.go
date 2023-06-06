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
			doorHassConfigTopic := "homeassistant/binary_sensor/matematdoor/config"
			token := c.Publish(doorHassConfigTopic, 2, true, `{"name":"Matemat Door", "availability_topic": "sielaff2mqtt/door/availability", "unique_id": "matematdoor", "state_topic": "sielaff2mqtt/door/state", "device_class": "door", "payload_on": "open", "payload_off":"closed"}`)
			token.Wait()

			token = c.Publish("sielaff2mqtt/door/availability", 2, true, "online")
			token.Wait()

			token = c.Publish("sielaff2mqtt/door/state", 2, false, "closed")
			token.Wait()

			// homeassistant auto discovery
			coolingTemperatureHassConfigTopic := "homeassistant/sensor/matematcoolingtemperature/config"
			token = c.Publish(coolingTemperatureHassConfigTopic, 2, true, `{"name":"Matemat Cooling Temperature", "availability_topic": "sielaff2mqtt/matematcoolingtemperature/availability", "unique_id": "matematcoolingtemperature", "state_topic": "sielaff2mqtt/matematcoolingtemperature/state", "device_class": "temperature", "unit_of_measurement": "°C"}`)
			token.Wait()

			token = c.Publish("sielaff2mqtt/matematcoolingtemperature/availability", 2, true, "online")
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

	sensorChan := make(chan *dbus.Signal, 10)
	dbusConn.Signal(sensorChan)
	// coroutine to handle dbus signal
	go func() {
		log.Println("dbus signal handler started")
		for v := range sensorChan {
			log.Println(v)

			if v.Name != "com.sielaff.siline.machine.MachineControl.sensorValue" {
				log.Println("ignored signal", v.Name)
				continue
			}
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

			token := client.Publish("sielaff2mqtt/matematcoolingtemperature/state", 2, false, fmt.Sprintf("%.2f", temp))
			token.Wait()
		}
	}()

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Everything is set up")
	<-done

	log.Println("Server Stopped")

	// unsubscribe dbus
	dbusConn.RemoveMatchSignal(matchSensorValue...)

	// unsubscribe mqtt
	token := client.Publish("sielaff2mqtt/door/availability", 2, true, "offline")
	token.Wait()

	token = client.Publish("sielaff2mqtt/matematcoolingtemperature/availability", 2, true, "offline")
	token.Wait()

	log.Println("All Devices Unsubscribed")

	client.Disconnect(250)

	dbusConn.Close()

	time.Sleep(1 * time.Second)
}
