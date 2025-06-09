import {Text, View} from 'react-native';
import {AudioCall} from "@/components/AudioCall/AudioCall";

export default function HomeScreen() {
    return (
        <View style={{flex: 1, height: "100%", paddingTop: "50%"}}>
            <AudioCall userId={'134134'}/>
            <Text style={{color: "white"}}>Test</Text>
        </View>
    );
}

