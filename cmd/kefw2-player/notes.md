# Adjusting brightnes on a raspberry pi

Displays are under `/sys/class/backlight`

```shell 
root@pidisplay:~# cat /sys/class/backlight/rpi_backlight/brightness
255
root@pidisplay:~# echo 25 > /sys/class/backlight/rpi_backlight/brightness
root@pidisplay:~# echo 255 > /sys/class/backlight/rpi_backlight/brightness
```
