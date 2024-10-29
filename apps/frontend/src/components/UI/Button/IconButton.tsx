import React, { FC, ReactNode } from "react";
import Button, { ButtonProps } from "./Button";

interface IconButtonProps extends ButtonProps {
    icon: ReactNode;
}

const IconButton: FC<IconButtonProps> = ({ icon, onClick, children, disabled, style }) => {
    return (
        <Button onClick={onClick} disabled={disabled} style={style}>
            {React.isValidElement(icon) && React.cloneElement(icon)}
            {children && <span className="ml-2">{children}</span>}
        </Button>
    );
};

export default IconButton;
