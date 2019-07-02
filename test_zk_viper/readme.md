## zookeeper & viper

watch config's content from zookeeper. sync remote config to local and then set to viper.

but i don't think viper is thread safe. maybe we should add lock for read and write op.
![link](https://github.com/wenfh2020/mytest/blob/master/pics/config_center_logic.png)