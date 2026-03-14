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
        </div>
    );
};

export default AdminPanel;
