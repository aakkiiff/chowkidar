TAG=0.1.5
# FE_IMG=aakkiiff/chowkidar:frontend-$TAG
BE_IMG=aakkiiff/chowkidar:server-$TAG
# AGNT_IMG=aakkiiff/chowkidar:agent-$TAG

# Build the images
docker build -t $BE_IMG ./server
# docker build -t $FE_IMG ./frontend
# docker build -t $AGNT_IMG ./agent

# Push the images to Docker Hub
docker push $BE_IMG
# docker push $FE_IMG
# docker push $AGNT_IMG

