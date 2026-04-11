export function providerLabel(provider?: string): string {
    switch (provider) {
    case 'azure-queue':
        return 'AZURE QUEUE';
    case 'azure-blob':
        return 'AZURE BLOB';
    default:
        return 'NATS';
    }
}
