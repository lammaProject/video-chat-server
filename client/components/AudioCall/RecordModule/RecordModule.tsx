import {AudioModule, RecordingPresets, useAudioRecorder} from "expo-audio";
import {useEffect, useRef, useState} from "react";
import {Alert, Button} from "react-native";

interface Props {
    isConnect: boolean;
    onAudioData: (audioData: ArrayBuffer | Blob) => void;
}

export const RecordModule = ({isConnect, onAudioData}: Props) => {
    const audioRecorder = useAudioRecorder(RecordingPresets.HIGH_QUALITY);
    const [isRecording, setIsRecording] = useState(false);
    const recordingIntervalRef = useRef<NodeJS.Timeout | null>(null);

    useEffect(() => {
        (async () => {
            const status = await AudioModule.requestRecordingPermissionsAsync();
            if (!status.granted) {
                Alert.alert('Permission to access microphone was denied');
            }
        })();
    }, []);

    useEffect(() => {
        if (isConnect) {
            void record();
        } else {
            void stopRecording();
        }

        return () => {
            if (recordingIntervalRef.current) {
                clearInterval(recordingIntervalRef.current);
            }
        };
    }, [isConnect]);

    const record = async () => {
        await audioRecorder.prepareToRecordAsync();
        await audioRecorder.record();
        setIsRecording(true);

        // Отправляем аудио чанками каждые 100мс
        recordingIntervalRef.current = setInterval(async () => {
            try {
                const uri = audioRecorder.getURI();
                if (uri) {
                    // Получаем аудио данные и отправляем
                    const response = await fetch(uri);
                    const blob = await response.blob();
                    onAudioData(blob);
                }
            } catch (error) {
                console.error('Error sending audio chunk:', error);
            }
        }, 100);
    };

    const stopRecording = async () => {
        if (recordingIntervalRef.current) {
            clearInterval(recordingIntervalRef.current);
            recordingIntervalRef.current = null;
        }

        if (audioRecorder.isRecording) {
            await audioRecorder.stop();
        }
        setIsRecording(false);
    };

    return (
        <Button
            title={isRecording ? 'Stop Microphone' : 'Start Microphone'}
            onPress={isRecording ? stopRecording : record}
        />
    );
};
