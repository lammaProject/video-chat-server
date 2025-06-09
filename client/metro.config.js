// metro.config.js
const {getDefaultConfig} = require('expo/metro-config');

const config = getDefaultConfig(__dirname);

config.resolver = {
    ...config.resolver,
    resolveRequest: (context, moduleName, platform) => {
        if (moduleName === 'event-target-shim' || moduleName === 'event-target-shim/index') {
            return {
                filePath: require.resolve('event-target-shim/dist/event-target-shim.js'),
                type: 'sourceFile',
            };
        }

        // Let Metro handle other modules normally
        return context.resolveRequest(context, moduleName, platform);
    },
};

module.exports = config;
