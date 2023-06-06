sielaff2mqtt
============

A simple go util to publish the state of a sielaff vending machine to mqtt - for integration into a [home assistant](https://www.home-assistant.io) installation.

Tested and used in the [temporärhaus](https://wiki.temporaerhaus.de/getraenkeautomat/siline) with a [Sielaff (Robimat) GF SiLine M](https://sielaff.de/produkte/vending/siline-serie) on firmware 0.13.6.

### Usage
Compilation:
```
GOARCH=arm GOARM=7 GOOS=linux go build main.go
```

Run on the vending machine:
```
MQTT_BROKER=tcp://192.168.0.10:1883 MQTT_USERNAME=matemat MQTT_PASSWORD=mqttpassword ./main
```