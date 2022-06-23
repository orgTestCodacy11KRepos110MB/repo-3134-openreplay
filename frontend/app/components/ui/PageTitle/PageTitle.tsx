import React from 'react';
import cn from 'classnames';

interface Props {
    title: string;
    className?: string;
}
function PageTitle({ title, actionButton = null, subTitle = '', className = '', subTitleClass }) {
    return (
        <div>
            <div className='flex items-center'>
                <h1 className={cn("text-2xl capitalize-first", className)}>
                    {title}
                </h1>
                { actionButton && actionButton}
            </div>
            {subTitle && <h2 className={cn("my-4 font-normal color-gray-dark", subTitleClass)}>{subTitle}</h2>}
        </div>
    );
}

export default PageTitle;
