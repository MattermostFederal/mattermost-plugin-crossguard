import manifest from 'manifest';
import React from 'react';

const AdminPanel = () => {
    return (
        <div style={{padding: '20px'}}>
            <h3>
                {manifest.name}
            </h3>
            <p>
                {'Version: '}
                {manifest.version}
            </p>
            <p>
                <a
                    href={`/plugins/${manifest.id}/public/help/help.html`}
                    target={'_blank'}
                    rel={'noopener noreferrer'}
                    style={{color: '#1C58D9', textDecoration: 'none', fontSize: '14px'}}
                >
                    {'View Cross Guard Documentation'}
                </a>
            </p>
        </div>
    );
};

export default AdminPanel;
